# Configuração

Precedência: **padrões** < **arquivo JSON** (`--config` ou `URNA_CONFIG`) <
**variáveis de ambiente** < **flags de linha de comando**.

O binário **não** carrega `.env` automaticamente. Exporte as variáveis, use um
gerenciador de secrets ou passe `--env-file`/`env_file` no Docker Compose.

## Seleção da eleição

| Variável | Flag | Padrão | Exemplo |
| --- | --- | --- | --- |
| `URNA_ELECTION` | `--election` | `6258` | `6258` (2º turno federal 2026) |
| `URNA_CONTEST` | `--contest` / `--cargo` | `1` | `1` Presidente, `3` Governador, `11` Prefeito |
| `URNA_UF` | `--uf` | `br` | `br`, `sp`, `am` |
| `URNA_MUNICIPALITY` | `--municipality` | vazio | `67458` |

Códigos vêm do config oficial (`/comum/config/ele-c.json`) e não podem ser
inventados. O cargo precisa existir no escopo escolhido; caso contrário o app
falha antes de fazer a coleta de resultados.

`URNA_UF=br` monitora o agregado nacional **federal** (ex.: Presidente). Para
cargos estaduais/municipais, informe a UF (e o município, quando aplicável); o
app recusa `br` para eleições não federais em vez de tentar um arquivo que não
existe. Uma troca de seleção exige um novo `URNA_STATE_PATH`.

## Coleta e resiliência

| Variável | JSON | Padrão | Observações |
| --- | --- | --- | --- |
| `URNA_POLL_INTERVAL` | `pollInterval` | `120s` | mínimo 30s, máximo 24h |
| `URNA_HTTP_TIMEOUT` | `httpTimeout` | `20s` | 2s–2m por requisição |
| `URNA_CONFIG_TTL` | `configTtl` | `30m` | cache do config comum |
| `URNA_MIN_BACKOFF` | `minBackoff` | `5s` | backoff entre ciclos |
| `URNA_MAX_BACKOFF` | `maxBackoff` | `5m` | teto do backoff |
| `URNA_MAX_RETRIES` | `maxRetries` | `2` | tentativas extras por ciclo |
| `URNA_MAX_FAILURES` | `maxFailures` | `3` | ciclos ruins até alerta |
| `URNA_RETENTION_EVENTS` | `retentionEvents` | `200` | eventos no estado |

O SPA do TSE atualiza a cada 120s. O mínimo de 30s acompanha a revalidação de
cache parcial do próprio SPA, mas **não** é recomendação de produção. Aumente o
intervalo em vez de reduzi-lo.

## Estado e observabilidade

| Variável | JSON | Padrão | Observações |
| --- | --- | --- | --- |
| `URNA_STATE_PATH` | `statePath` | `./data/state.json` | um escritor por arquivo |
| `URNA_TUI` | `tui` | `true` | desligada se stdout não for TTY |
| `URNA_LOG_LEVEL` | `logLevel` | `info` | `debug`, `info`, `warn`, `error` |
| `URNA_LOG_FORMAT` | `logFormat` | `text` | `text` ou `json` |
| `URNA_LOG_FILE` | `logFile` | vazio | se vazio e TUI, usa `data/urna-watch.log` |
| `URNA_STATUS_ADDR` | `statusAddr` | vazio | `host:porta`; ativa `/healthz` e `/status` |
| `URNA_TIMEZONE` | `timezone` | `America/Sao_Paulo` | exibição |
| `URNA_USER_AGENT` | `userAgent` | `urna-watch/0.1 (...)` | identifique seu deploy |

Em TUI, logs vão para arquivo para não corromper a tela. Em headless, vão para
stderr. O arquivo de log é criado com `0600`.

## Fonte

| Variável | JSON | Padrão |
| --- | --- | --- |
| `URNA_TSE_BASE_URL` | `tseBaseUrl` | `https://resultados.tse.jus.br` |
| `URNA_TSE_ENVIRONMENT` | `tseEnvironment` | `oficial` |

Alterar a URL só faz sentido para testes locais/espelhos. O ambiente precisa ser
um segmento de caminho válido (`[A-Za-z0-9._-]`). Em produção use HTTPS.

## Telegram

| Variável | JSON | Padrão |
| --- | --- | --- |
| `TELEGRAM_ENABLED` | `telegram.enabled` | `false` |
| `TELEGRAM_BOT_TOKEN` | `telegram.botToken` | vazio (segredo) |
| `TELEGRAM_CHAT_ID` | `telegram.chatId` | vazio |
| `TELEGRAM_API_BASE` | `telegram.apiBase` | `https://api.telegram.org` |
| `TELEGRAM_BASELINE` | `telegram.baseline` | `true` |
| `TELEGRAM_CHARTS` | `telegram.charts` | `true` (gráfico PNG via `sendPhoto`) |
| `TELEGRAM_MAPS` | `telegram.maps` | `true` (mapa por UF vencedora; 27 arquivos com cache) |
| `TELEGRAM_FAILURE_ALERTS` | `telegram.failureAlerts` | `true` |
| `TELEGRAM_RECOVERY_ALERTS` | `telegram.recoveryAlerts` | `true` |
| `TELEGRAM_MAX_ATTEMPTS` | `telegram.maxAttempts` | `8` |
| `TELEGRAM_BASE_BACKOFF` | `telegram.baseBackoff` | `5s` |

Com `TELEGRAM_ENABLED=true`, token e chat id são obrigatórios. Se o Telegram for
desligado com fila pendente, as linhas são marcadas como suprimidas para não
serem enviadas tardiamente após uma reativação.

**Segurança:** prefira o token apenas em variável de ambiente/secret manager.
`config.example.json` existe por completude, mas um arquivo local com token deve
ser ignorado pelo Git. `docs/security.md` detalha rotação.

## Arquivo JSON

Exemplo completo em [`config.example.json`](../config.example.json). Campos
desconhecidos são rejeitados (`DisallowUnknownFields`). Durações aceitam string
Go (`"120s"`, `"5m"`) ou número em nanossegundos.

```sh
./bin/urna-watch --config config.example.json
```

## Flags

```text
--config PATH          arquivo JSON
--election CODE
--contest CODE         --cargo é alias
--uf UF
--municipality CODE
--state PATH
--poll-interval DUR
--http-timeout DUR
--status-addr HOST:PORT
--log-level LEVEL
--log-format text|json
--no-tui
--no-telegram
--once                 coleta única, sem persistir/notificar
--json                 com --once, saída JSON
--validate             valida config/endpoint e sai
--version
```

`--once` e `--validate` desativam o Telegram deliberadamente.

## Exemplos

2º turno presidencial 2026, headless, estado local:

```sh
URNA_ELECTION=6258 URNA_CONTEST=1 URNA_UF=br \
URNA_TUI=false URNA_STATE_PATH=./data/presidente-2026.json \
./bin/urna-watch --no-tui
```

Governador do Amazonas (2º turno 2026):

```sh
URNA_ELECTION=6260 URNA_CONTEST=3 URNA_UF=am \
URNA_STATE_PATH=./data/governador-am-2026.json \
./bin/urna-watch
```

Município de Narandiba/SP (suplementar 2026):

```sh
URNA_ELECTION=6284 URNA_CONTEST=11 URNA_UF=sp URNA_MUNICIPALITY=67458 \
URNA_STATE_PATH=./data/narandiba-2026.json \
./bin/urna-watch
```

## Validação no início

O app valida toda a configuração antes de abrir a rede: códigos, UF, limites de
intervalo/backoff, nível/formato de log, timezone, endereço de status, caminho
de estado e credenciais do Telegram. Erros são exibidos sem o token.
