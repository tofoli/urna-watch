# ADR 0005 — Persistência

- Status: aceita
- Data: 2026-10-25

## Contexto

Precisamos persistir último snapshot válido, validadores HTTP, eventos e uma
fila durável de notificações, com recuperação após reinício. O volume é baixo:
uma eleição/cargo/abrangência por estado, poucos eventos e poucas mensagens.

## Critérios

- durabilidade e escrita atômica;
- recuperação simples;
- dedupe de eventos/notificações;
- baixo custo operacional e de dependências;
- backup/inspeção easy;
- um escritor por arquivo.

## Alternativas

- **SQLite** (`mattn/go-sqlite3` ou `modernc.org/sqlite`): consultas,
  concorrência e migrações, mas adiciona CGO/driver, esquema e complexidade para
  um estado pequeno de escritor único.
- **BoltDB/bbolt**: chave-valor, transações; dependência extra e sem ganho
  claro sobre arquivo atômico.
- **JSON atômico**: simples, auditável, sem dependência; adequado ao volume.

## Decisão

Estado JSON único, carregado em memória, protegido por mutex, com escrita
`temp + chmod 0600 + write + fsync + rename + fsync(dir)`. Estado versionado
(`StateVersion`), retenção de eventos e poda do outbox.

Estrutura (resumo): `ref`, `snapshot`, `cache` (ETag/Last-Modified), `events`,
`queue`, `lastSuccess`, `lastFailure`, `consecutiveFailures`.

## Consequências

Positivas:

- zero CGO/banco, deploy trivial;
- inspeção e backup com `jq`/`cp`;
- escrita atômica testada.

Negativas:

- sem consultas/indexação; todo o estado é carregado;
- um único processo escritor (documentado);
- migrações manuais por versão;
- arquivo pode crescer com muitos eventos/outbox (retenção mitiga).

## Critérios para migrar para SQLite

- múltiplas eleições/abrangências concorrentes na mesma instância;
- consultas históricas complexas;
- volume de eventos/snapshots que torne o carregamento total custoso;
- múltiplos processos precisando compartilhar estado.

Nesse caso, criar ADR substituta, migração explícita e testes de recuperação.

## Riscos

- Corrupção externa falha alto; requer backup/remoção consciente.
- Perder o arquivo perde dedupe/histórico, mas não corrompe a fonte.

## Referências

- Implementação: `internal/store`.
- Testes: `internal/store/file_test.go`, `tests/integration`.
