# Desenvolvimento

## Ferramentas

| Ferramenta | Versão usada | Observação |
| --- | --- | --- |
| Go | 1.27.3 | `go.mod` declara `go 1.27.3` |
| GNU Make | 4.x | alvos em `Makefile` |
| golangci-lint | 2.x (opcional) | `make lint` usa se estiver no PATH |
| Docker | 29.x (opcional) | build/Compose |
| git | qualquer recente | `make build` usa versão/commit se houver repo |

Instalação do Go: https://go.dev/dl/. O projeto não usa CGO nem banco nativo.

## Setup

```sh
git clone <repo> urna-watch
cd urna-watch
cp .env.example .env     # opcional; não versionar
go mod download
make check
```

Sem venv/npm. Dependências diretas: Bubble Tea, Lip Gloss e `x/ansi` (usado
para largura/truncamento ANSI).

## Comandos

```sh
make build        # bin/urna-watch
make test         # go test ./...
make test-race    # go test -race ./...
make cover        # coverage.out + resumo
make fmt          # gofmt -w
make fmt-check    # falha se gofmt for necessário
make lint         # go vet + golangci-lint opcional
make check        # fmt-check + lint + test + build
make run
make once
make validate
make docker-build
```

Testes individuais:

```sh
go test ./internal/tse -run TestParseResult -v
go test ./tests/integration -v
go test ./internal/tui -run TestViewWideAndNarrow -v
```

## Organização

```text
cmd/urna-watch        main, flags, sinais, seleção de modo
internal/config       env/JSON/defaults/validação
internal/tse          cliente HTTP, config oficial, parser EA20
internal/domain       modelo + mudanças + eventos
internal/detector     diff normalizado
internal/polling      agenda/retry/backoff/comandos
internal/store        estado JSON atômico + outbox
internal/notification mensagens e worker
internal/telegram     Bot API
internal/tui          dashboard
internal/observability logger, Status, HTTP de saúde
internal/app          orquestração
tests/fixtures/tse    fixtures reais (públicas)
tests/integration     pipeline com httptest
```

## Fluxo de mudança

1. Leia `AGENTS.md` e o ADR relacionado.
2. Para mudança na fonte, siga `docs/research/tse-data-source.md` e valide com
   Browser Harness + `curl` antes de tocar no parser.
3. Escreva/ajuste testes primeiro quando possível.
4. Rode `make check` e `go test -race ./...`.
5. Atualize README/docs/CHANGELOG/ADRs afetados no mesmo commit.
6. Use Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`).

## Fixtures

- Reais e públicas em `tests/fixtures/tse/`, com hashes e data no README local.
- Não edite uma fixture real para simular mudança. Gere a variação em memória
  (`mutateJSON` nos testes) e mantenha a origem intacta.
- Se adicionar uma fixture, registre URL, horário UTC, hash e atualize
  `docs/research/tse-data-source.md`.

## Adicionando dependência

Verifique, nesta ordem:

1. a biblioteca padrão resolve?
2. licença compatível (MIT/Apache/BSD), manutenção ativa, tamanho e CGO;
3. se estrutural, registre ADR;
4. rode `go mod tidy` e `go test ./...`.

Não adicione framework web, ORM, cliente SQLite ou biblioteca de logging
alternativa sem uma necessidade demonstrada.

## Debug

- `URNA_LOG_LEVEL=debug` mostra 304s e decisões de coleta.
- `--once --json` inspeciona o snapshot sem tocar no estado/Telegram.
- `--validate` confirma config, endpoint e normalização.
- `URNA_STATUS_ADDR=127.0.0.1:8080` expõe `/status` (sem segredos).
- Em TUI, logs vão para `data/urna-watch.log`; use `tail -f`.
- Nunca habilite `httptest`/curl apontando para o grupo real em testes da suíte.

## Convenções de commit

Mensagens curtas e imperativas, corpo com o “porquê”, evidência de testes.
Exemplo:

```text
fix(tse): reject snapshots older than the stored one

CDN edges can serve an older generation after a newer one. Keep the last
good snapshot and retry with backoff instead of emitting a false revision.

Tested: go test ./... ; go test -race ./...
```
