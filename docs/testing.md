# Testes

## Suíte padrão (offline)

```sh
make test
make test-race
```

A suíte usa apenas `httptest`, arquivos locais e relógio real. **Não** acessa TSE
nem Telegram e **não** exige token. Fixtures reais ficam em
`tests/fixtures/tse/`.

## Cobertura por área

| Pacote | O que é testado |
| --- | --- |
| `internal/tse` | parsing dos fixtures 6258/6257/6260/6284; JSON inválido; `idg` ausente; votos negativos; `tf=s` com seções pendentes; percentual exato; resolução de config; URLs br/UF/município; escopo inválido; ETag/304; 429/Retry-After; 404; config fora; cache |
| `internal/detector` | baseline; nenhuma mudança; metadados; votos; percentual; seções/totais; revisão; final; candidato removido; ordem de JSON; situação; ID determinístico |
| `internal/store` | atomicidade/permissões; reabertura; corrupção; rollback; retenção/dedupe de eventos; fila; due/retry; supressão |
| `internal/polling` | intervalo mínimo; primeira execução; métricas; erro permanente; retry+sucesso; `Retry-After`; pause/resume; `OnCycle`; 304 |
| `internal/notification` | mensagem curta com valores reais; link humano (não JSON); atualização/revisão/final; escape HTML; timestamp desconhecido; falha/recuperação; split determinístico; rows de foto |
| `internal/telegram` | envio; parse mode; `sendPhoto` multipart; 429; erro permanente com token redigido; 5xx; validação; split UTF-16/emoji/linha longa |
| `internal/chart` | PNG válido/dimensões; barras; mapa com 27 UFs; vencedores por estado; sem votos retorna `ErrNoData` |
| `internal/config` | defaults; env; JSON estrito; validação; redaction do chat id |
| `internal/tui` | dimensões 120x40, 100x30, 80x24, 40x12; largura/altura; teclado; mouse/roleta; detalhes; navegação; ajuda; seletor/visão por UF; snapshot vazio |
| `tests/integration` | pipeline completo com TSE simulado: baseline, 304, mudança, dedupe, persistência, gráfico via `sendPhoto` no Telegram fake, outbox `sent`, visão por UF somente leitura |

## Fixtures

- `br-c0001-e006258-u.json`: 2º turno presidencial 2026, `tf=n`, 1.225/499.248
  seções, 345.054 votos.
- `br-c0001-e006257-u.json`: 1º turno 2026, `tf=s`, qualificados ao 2º turno.
- `am-c0003-e006260-u.json`: governador AM, 2º turno.
- `sp67458-c0011-e006284-u.json`: municipal suplementar, 0 seções, `dt/ht`
  vazios.
- `br-e006257-ab.json`: agregado por UF.
- `ele-c.json`: config oficial.
- `*.jws.txt`: JWS compacto usado pelo SPA.

Hashtags/fonte no README local. Variações são geradas em memória; fixtures reais
não são editadas.

## Como testar uma mudança de parser

1. Adicione/atualize fixture **real** com origem/data/hash.
2. Reproduza a URL com `curl` e confirme os valores na página oficial.
3. Crie teste de sucesso + pelo menos um caso inválido.
4. Gere variações em memória para mudança (votos, seções, `tf`).
5. Rode `go test ./internal/tse -v` e `go test ./...`.
6. Atualize `docs/research/tse-data-source.md` se o esquema/endpoint mudou.

## Como testar notificações sem enviar ao grupo real

- Use os testes de `internal/notification` e `internal/telegram` (httptest).
- Use `--no-telegram` para coleta/validação.
- No pipeline de integração, o servidor fake valida path/body e retorna
  `{"ok":true,...}`.
- Nunca coloque token real em teste, fixture ou CI.

## Execução real (manual, opcional)

```sh
# não persiste nem notifica
./bin/urna-watch --validate --no-tui
./bin/urna-watch --once --json --no-tui --no-telegram | jq .

# com Telegram em estado temporário, para smoke test
TELEGRAM_ENABLED=true TELEGRAM_BOT_TOKEN=... TELEGRAM_CHAT_ID=... \
URNA_STATE_PATH=/tmp/urna-smoke/state.json URNA_TUI=false \
timeout 15s ./bin/urna-watch --no-tui
```

Um smoke test real foi executado em 2026-10-25 contra o 2º turno presidencial e
o grupo configurado; a mensagem de linha de base foi confirmada como `sent` no
estado e o log registrou `notificação enviada ao Telegram`.

## Limitações da suíte

- Não valida assinatura JWS.
- Não testa rate limit real do TSE (deliberadamente).
- Não testa 2024 (endpoints testados retornaram 404; ver pesquisa).
- A TUI é testada por renderização/lógica, não por screenshot de terminal real.
- O teste de Telegram com API real é manual e depende de grupo/token externos.
- Longa duração/restart real do processo não tem teste automatizado; a
  persistência/retomada é coberta por reabertura de estado e fila.

## Evidências executadas em 2026-10-25

| Verificação | Resultado |
| --- | --- |
| `gofmt -l cmd internal tests` | sem saída |
| `go vet ./...` | OK |
| `go test ./...` | todos os pacotes OK |
| `go test -race ./...` | todos os pacotes OK |
| `make check` | OK (fmt-check + vet + test + build) |
| `govulncheck ./...` | 0 vulnerabilidades chamadas (1 em módulo, não chamada) |
| `docker build -t urna-watch:local .` | OK |
| container non-root com volume nomeado | `/healthz` 200; estado `0600`; snapshot 3948414 |
| TUI em PTY (`script`) | renderizou candidatos/painéis sem panic |
| smoke real TSE + Telegram | linha de base entregue; estado `sent` |

O smoke real usou a fonte oficial e um grupo privado; o token/chat id **não**
foram registrados no repositório. A mensagem foi confirmada pelo log
`notificação enviada ao Telegram` e pela fila `sent` no estado temporário.
