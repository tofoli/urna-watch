# Arquitetura

## Visão geral

Urna Watch é um binário Go único com dois modos de apresentação (TUI e headless)
e um pipeline de dados independente da interface.

```text
                  ┌──────────────────────────────────────────────────┐
                  │                    cmd/urna-watch                │
                  │  flags · sinais · logger · once/validate/TUI     │
                  └───────────────┬──────────────────────────────────┘
                                  │
                  ┌───────────────▼──────────────────────────────────┐
                  │                    internal/app                  │
                  │  wiring · baseline · falhas · persistência       │
                  └───┬───────────┬───────────┬───────────┬──────────┘
                      │           │           │           │
              ┌───────▼──┐ ┌──────▼─────┐ ┌───▼──────┐ ┌──▼──────────┐
              │ tse      │ │ detector   │ │ store    │ │ notification│
              │ adapter  │ │ compare    │ │ JSON+out │ │ formatter   │
              └────┬─────┘ └────────────┘ └─────┬────┘ └──────┬──────┘
                   │                            │             │
                   │                     ┌──────▼─────┐ ┌─────▼──────┐
                   │                     │ polling    │ │ telegram   │
                   │                     │ schedule   │ │ Bot API    │
                   │                     └────────────┘ └────────────┘
                   │
          ┌────────▼─────────┐       ┌──────────────────────────────┐
          │ domain (puro)    │◄──────│ observability (Status/log)   │
          └──────────────────┘       └──────────────┬───────────────┘
                                                   │
                                         ┌─────────▼─────────┐
                                         │ tui (Bubble Tea)  │
                                         │ /status HTTP      │
                                         └───────────────────┘
```

A coleta nunca importa `tui`. A TUI nunca faz rede; ela consulta
`observability.Status` a cada 500 ms e envia comandos `Pause/Resume/Refresh`
para o poller.

## Componentes

### `internal/tse`

- `Adapter` resolve o config oficial, valida a seleção e baixa o arquivo
  `*-u.json` com `If-None-Match`/`If-Modified-Since`.
- O config comum é cacheado em memória por 30 min (configurável); um erro de
  config usa a cópia em memória se existir.
- `ParseResult` é puro, sem rede. Converte strings EA20 para o modelo interno,
  valida invariantes e calcula o ID determinístico do snapshot.
- Não interpreta HTML. O `.jws` é documentado, mas o coletor usa o `.json`
  equivalente.

### `internal/domain`

- `Measure` distingue ausente de zero.
- `Snapshot` é imutável após a construção. `Fingerprint` exclui horário de
  captura e metadados HTTP.
- `ChangeSet` contém itens normalizados, revisão e `EventID`.
- `ElectionInfo` carrega nomes oficiais, turno, abrangência e URL da fonte.

### `internal/detector`

- Compara candidatos por número, não por ordem do JSON.
- Compara votos, percentual exato (nano), posição, situação/elegibilidade,
  seções, totais e transições `tf`/`dv`.
- Decrementos marcam `Revision=true`; o evento é rotulado como revisão.
- Nenhum item significa nenhum evento, mesmo com `idg`/timestamps diferentes.

### `internal/polling`

- Primeira coleta imediata; depois `interval` com jitter de ±5%.
- Até `MaxRetries` tentativas por ciclo, com backoff curto.
- Falha de ciclo usa backoff exponencial até `MaxBackoff`.
- `Retry-After` interrompe o ciclo e define a próxima espera.
- `Pause`/`Resume`/`Refresh` via canal; a coleta em andamento não é abortada.
- `OnCycle` informa sucesso/falha final ao app (uma vez por ciclo).

### `internal/store`

- Um `FileStore` single-writer, `sync.Mutex`, estado em memória.
- Cada `Update` faz cópia, muta, serializa, escreve temp, `fsync`, `rename` e
  `fsync` do diretório.
- Estado: versão, seleção, snapshot, cache de ETag, eventos (retenção),
  outbox, contadores de falha.
- Corrupção falha alto; não há reset silencioso.

### `internal/notification` + `internal/telegram`

- O formatter gera mensagens curtas em HTML pt-BR; todos os dados vêm do
  snapshot e são escapados. O link é a página humana oficial, não o JSON.
- Com gráficos habilitados, `internal/chart` gera um PNG e o outbox usa
  `sendPhoto` com a mensagem como legenda; falhas caem para texto.
- Para eleição federal/Brasil com `TELEGRAM_MAPS=true`, o app busca os 27
  arquivos `-u.json` de UF (concorrência 4, cache 60s, ETag), calcula o
  vencedor em cada estado e desenha um mapa do Brasil (malha oficial do IBGE)
  ao lado das barras dos candidatos. O mapa não altera snapshot/detecção.
- Mensagens longas são divididas em chunks ≤4096 unidades UTF-16, com IDs
  determinísticos `<eventID>-<n>`.
- O worker processa linhas devidas, atualiza tentativas/erro/próxima tentativa e
  marca `sent` ou `failed`.
- O cliente Telegram nunca coloca o token em erro/log; 429/5xx são transitórios,
  4xx permanente.
- Entrega **at-least-once**; dedupe lógica por evento e chunks.

### `internal/tui`

- Bubble Tea com alt screen e mouse.
- Layout largo (tabela à esquerda; apuração/eventos à direita) e empilhado em
  terminais estreitos.
- Hitboxes registradas no render para clique/roda.
- Modais de ajuda, candidato, evento e seletor de UF.
- Visão por UF sob demanda: `App.SelectUF` busca o `-u.json` da UF em uma
  goroutine separada e expõe um snapshot somente leitura em `Status.UFView`;
  não altera snapshot, detector, outbox ou notificações.
- Sempre mostra “em apuração”/“totalização final” conforme `tf`; nunca inventa
  progresso.
- Se stdout não é TTY, o app opera headless.

### `internal/observability`

- Logger `slog` com redação de chaves sensíveis.
- `Status` serializável para `/status`, sem token.
- `/healthz`: 200 quando pausado, iniciando ou com coleta recente; 503 quando
  nunca houve sucesso ou o último sucesso está velho.
- Servidor HTTP opcional com timeouts; sem escrita.

## Ciclo de coleta

```text
1. Lê estado + cache da seleção
2. Resolve config oficial (cache/ETag)
3. GET condicional do arquivo de resultados
4. 304? atualiza sucesso/cache e verifica recuperação; fim
5. Parse + validação estrutural
6. Snapshot mais antigo que o armazenado? rejeita, preserva o bom, backoff
7. Compara snapshots
8. Monta evento(s) e mensagens; linha de base na primeira coleta
9. Persiste snapshot/cache/eventos/outbox atomicamente
10. Acorda o worker do Telegram
```

## Falhas e concorrência

- A TUI não bloqueia rede; o poller roda no `app.Run`.
- O worker do Telegram é independente do poller; uma falha de envio não afeta a
  coleta.
- O `Status` é montado a partir de cópias (store + métricas atômicas).
- Encerramento: `signal.NotifyContext` → poller para → worker termina o envio em
  andamento (timeout) → estado consistente em disco.
- O estado suporta **um** processo escritor por arquivo.

## Modelo de estado (resumo)

```json
{
  "version": 1,
  "ref": {"cycle":"ele2026","electionCode":"6258","contestCode":"1","scope":{...}},
  "snapshot": {"sourceVersion":"3948414","isFinal":false,"candidates":[...]},
  "cache": {"6258|1|br:br": {"etag":"...","lastModified":"..."}},
  "events": [{"id":"...","summary":"...","notificationStatus":"sent"}],
  "queue": [{"id":"...-0","state":"sent","attempts":1}],
  "lastSuccess": "...",
  "consecutiveFailures": 0
}
```

## Decisões relacionadas

- [ADR 0001](adr/0001-language-and-runtime.md) — Go.
- [ADR 0002](adr/0002-terminal-ui.md) — TUI.
- [ADR 0003](adr/0003-tse-data-source.md) — fonte EA20.
- [ADR 0004](adr/0004-polling-and-change-detection.md) — polling/detecção.
- [ADR 0005](adr/0005-persistence.md) — JSON em vez de SQLite.
- [ADR 0006](adr/0006-telegram-delivery.md) — entrega.
- [ADR 0007](adr/0007-deployment.md) — Docker/CI.
