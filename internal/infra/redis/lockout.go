// Rate-limit/lockout de login por (e-mail, IP): contador de falhas numa
// janela e bloqueio temporário quando o teto estoura. Implementação REDIS —
// sem Redis NÃO EXISTE lockout (degradável, agents/02): a ausência do
// cliente faz todos os métodos virarem no-op, e o consumidor segue o fluxo
// normal. A janela/bloqueio vêm da config (cache.login_lockout), nunca de
// constante.
//
// Desenho de chaves: lock:c:{hash} é o contador (TTL = janela; a primeira
// falha abre a janela) e lock:b:{hash} é o marcador do bloqueio (TTL =
// bloqueio), gravado só quando o contador atinge o teto — Bloqueado olha
// apenas o marcador, então contar falhas abaixo do teto nunca prende ninguém.
package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// LimitadorLogin agrega os parâmetros da política. Cliente nil = degradado.
type LimitadorLogin struct {
	cliente       *goredis.Client
	maxTentativas int
	janela        time.Duration
	bloqueio      time.Duration
}

// NovoLimitadorLogin monta o limitador; parâmetros vêm da config já com
// defaults aplicados (pkg/config).
func NovoLimitadorLogin(cliente *goredis.Client, maxTentativas int, janela, bloqueio time.Duration) *LimitadorLogin {
	return &LimitadorLogin{cliente: cliente, maxTentativas: maxTentativas, janela: janela, bloqueio: bloqueio}
}

// Bloqueado diz se o par está preso no bloqueio e quanto falta. Degradado =
// liberado SEM erro; falha de Redis sobe para o chamador decidir (o
// adaptador do bootstrap trata: falhar aberto aqui seria derrubar o login
// inteiro por causa do cache — a recusa certa é seguir sem lockout).
func (l *LimitadorLogin) Bloqueado(ctx context.Context, email, ip string) (bool, time.Duration, error) {
	if l.cliente == nil {
		return false, 0, nil
	}
	ttl, err := l.cliente.TTL(ctx, ChaveLockBloqueio(email, ip)).Result()
	if err != nil {
		return false, 0, err
	}
	if ttl <= 0 { // -2 = chave inexistente; -1 = sem TTL (não deve ocorrer) → liberado
		return false, 0, nil
	}
	return true, ttl, nil
}

// RegistrarFalha incrementa o contador na janela; atingir o teto aplica o
// bloqueio (e zera o contador — quem governa agora é o marcador). No-op
// quando degradado.
func (l *LimitadorLogin) RegistrarFalha(ctx context.Context, email, ip string) error {
	if l.cliente == nil {
		return nil
	}
	chave := ChaveLockContador(email, ip)
	var contagem int64
	err := l.cliente.Watch(ctx, func(tx *goredis.Tx) error {
		pipeline := tx.Pipeline()
		incr := pipeline.Incr(ctx, chave)
		if err := tx.Expire(ctx, chave, l.janela).Err(); err != nil {
			return err
		}
		if _, err := pipeline.Exec(ctx); err != nil {
			return err
		}
		contagem = incr.Val()
		return nil
	}, chave)
	if err != nil {
		return err
	}
	if contagem < int64(l.maxTentativas) {
		return nil
	}
	if err := l.cliente.Set(ctx, ChaveLockBloqueio(email, ip), "1", l.bloqueio).Err(); err != nil {
		return err
	}
	return l.cliente.Del(ctx, chave).Err()
}

// RegistrarSucesso limpa o histórico do par (login bom zera falhas e sai de
// bloqueio pendente de contagem — o marcador de bloqueio NÃO é removido:
// cumprir a pena é inegociável). No-op quando degradado.
func (l *LimitadorLogin) RegistrarSucesso(ctx context.Context, email, ip string) error {
	if l.cliente == nil {
		return nil
	}
	return l.cliente.Del(ctx, ChaveLockContador(email, ip)).Err()
}
