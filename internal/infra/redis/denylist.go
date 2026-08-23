// Denylist do JWT em Redis: CACHE da revogação persistida — a verdade é a
// tabela identidade_user_refresh_token (subdomínio user), consultada quando
// o cache não responde. Implementa ESTRUTURALMENTE o contrato
// RevogadorDeRefresh declarado em internal/infra/jwt SEM importá-lo (infra
// não importa infra — regra 2 de agents/01); a ligação acontece no
// cmd/bootstrap, que compõe cache + fonte.
//
// Política de segurança: só resultado POSITIVO é cacheado. Revogação é
// permanente (a linha nunca é removida), então o positivo pode viver até o
// TTL do refresh; o NEGATIVO nunca é cacheado — logout e rotação valem NA
// HORA sem precisar de invalidação, e um Redis vazio nunca libera token.
package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const valorRevogado = "1"

// Denylist guarda o cliente e o TTL das entradas (padrão: TTL do refresh —
// depois dele o jti não é mais apresentável, então não há que lembrar).
type Denylist struct {
	cliente *goredis.Client
	ttl     time.Duration
}

// NovaDenylist monta o cache; cliente nil é aceito (degradado) e todos os
// métodos viram no-op — o consumidor composto segue à fonte.
func NovaDenylist(cliente *goredis.Client, ttl time.Duration) *Denylist {
	return &Denylist{cliente: cliente, ttl: ttl}
}

// Revogado confere o jti no cache. Degradado/ausente = false SEM erro (o
// chamador consulta o Postgres); falha real de Redis sobe para o chamador
// decidir — o adaptador composto do bootstrap trata caindo à fonte.
func (d *Denylist) Revogado(jti string) (bool, error) {
	if d.cliente == nil {
		return false, nil
	}
	valor, err := d.cliente.Get(context.Background(), ChaveDenylistJWT(jti)).Result()
	if err == goredis.Nil {
		return false, nil // ausente ≠ revogado: decisão é da fonte
	}
	if err != nil {
		return false, err
	}
	return valor == valorRevogado, nil
}

// MarcarRevogado grava o positivo com TTL. No-op quando degradado.
func (d *Denylist) MarcarRevogado(jti string) error {
	if d.cliente == nil {
		return nil
	}
	return d.cliente.Set(context.Background(), ChaveDenylistJWT(jti), valorRevogado, d.ttl).Err()
}
