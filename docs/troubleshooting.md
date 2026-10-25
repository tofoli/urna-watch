# Troubleshooting

## A coleta retorna 404

- O arquivo pode ainda não ter sido publicado para a eleição/cargo/abrangência.
- Confirme os códigos no config oficial:

  ```sh
  curl -sS https://resultados.tse.jus.br/oficial/comum/config/ele-c.json \
    | jq '.pl[] | {cd, c, e: [.e[] | {cd, nm, t, tp, abr: [.abr[].cd]}]}'
  ```

- Confirme a URL construída (`--validate` imprime o endpoint).
- Não faça brute-force nem reduza o intervalo. 404 não vira snapshot vazio.
- `UF=br` com cargo estadual/municipal é rejeitado de propósito; informe a UF.
- Para município, confirme o código de 5 dígitos em:
  `.../<ciclo>/<eleicao>/config/mun-e<eleicao6>-cm.json`.
- Eleições antigas: os caminhos de 2024 testados retornaram 404 em 2026-10-25;
  o suporte validado é o de 2026.

## A página mostra dados novos, mas o app mostra “sem alterações”

- Pode ser `HTTP 304`: o arquivo não mudou de geração.
- A página pode estar exibindo cache local enquanto a CDN entrega a geração
  anterior. Faça `--once --json` e compare `sourceVersion`/`dt/ht`.
- `dt/ht` constante ≠ falha; a fonte pode não ter novas seções.
- O app compara dados normalizados, não o horário de geração. Mudança só de
  `dg/hg`/`idg` sem alteração de votos/seções não gera evento.
- Se o snapshot recebido for **mais antigo** que o armazenado, o app preserva o
  bom e tenta novamente.

## A página mostra conclusão, mas o app diz “em apuração”

O app só declara totalização final com `tf=s`. `pst=100` isolado não basta.
Verifique `tf` no JSON. Se a fonte mudou a semântica, abra uma investigação e
atualize parser/testes/ADR.

## 429 / Too Many Requests

- Respeite `Retry-After`; o poller já pausa o ciclo e agenda a próxima tentativa.
- Aumente `URNA_POLL_INTERVAL`; o padrão é 120s.
- Verifique se outra instância está usando o mesmo IP/intervalo.
- Não implemente “retry imediato” em loop.

## 5xx, timeouts e DNS

- São transitórios; o poller tenta novamente com backoff.
- Verifique relógio do host, proxy e CA (`ca-certificates` no container).
- `curl -v https://resultados.tse.jus.br/oficial/comum/config/ele-c.json`.
- Um erro de config usa a cópia em memória se já carregada; reinício sem cache
  exige a fonte disponível.

## JSON inválido / “resposta inválida do TSE”

- O app rejeita HTML, JSON truncado, campos ausentes e invariantes quebradas.
- O último snapshot bom é preservado; o erro aparece no log/status.
- Salve o corpo ofensor (sem credenciais), compare com a fixture e abra uma
  investigação. Não “conserte” ignorando campos sem evidência.
- Limite de corpo: 32 MiB.

## Estado corrompido ou “pertence a outra eleição”

- Corrompido: o app falha alto. Faça backup e remova/renomeie o arquivo:

  ```sh
  mv data/state.json data/state.corrupt.$(date +%s).json
  ```

  Isso perde dedupe/histórico e recria a linha de base.

- Outra eleição: use um `URNA_STATE_PATH` distinto por seleção. Não edite o
  arquivo para “forçar” a troca.

- Não rode duas instâncias no mesmo arquivo. Há um escritor por arquivo.

## Telegram não envia

- `TELEGRAM_ENABLED=true` exige token e chat id válidos.
- Teste o token: `getMe`. Teste o chat: envie `sendMessage` com um texto curto.
- Bot fora do grupo ou sem permissão de enviar → 400/403 permanente (não
  retenta).
- 429: o worker respeita `Retry-After`; não reinicie em loop.
- Verifique `data/state.json` → `queue[].lastError` (não contém token).
- O bot não vê mensagens antigas do grupo; para descobrir o chat id, use
  `getUpdates` após adicionar o bot e enviar algo no grupo.
- Se o Telegram foi desligado com fila pendente, as antigas são suprimidas; a
  próxima mudança gera uma nova.

## TUI estranha/corrompida

- Use um terminal moderno com UTF-8 e suporte a 256 cores.
- Se stdout não é TTY, o app cai em headless; force `--no-tui` em scripts/CI.
- Logs nunca vão para stderr em TUI (vão para arquivo), então não devem
  quebrar a tela.
- Em terminal muito pequeno, o layout compacta/trunca; redimensione.
- Se o mouse não funcionar, o terminal/host pode não repassar eventos; o teclado
  continua disponível.

## `--validate` falha antes da rede

- Códigos/UF/cargo inconsistentes com o config oficial.
- Telegram habilitado sem token/chat: `--validate` desliga o Telegram, mas a
  validação de config inicial ainda pode exigir consistência; use
  `TELEGRAM_ENABLED=false` ou `--no-telegram`.
- Timezone/intervalo/backoff inválidos são rejeitados.

## Logs têm `[redacted]`

Esperado. O logger e o cliente Telegram removem tokens de mensagens/erros. Se
um token aparecer em texto livre, revogue-o e abra um bug.

## CI falha em `gofmt`

Rode `make fmt` e verifique se editores não converteram tabs. O CI usa
`gofmt -l cmd internal tests`.

## O container não sobe

- `env_file: .env` é opcional no Compose; sem ele, use variáveis exportadas.
- O volume nomeado deve ser gravável pelo UID do container (100). Se usar bind
  mount, rode `mkdir -p data && sudo chown -R 100:101 data`.
- Com raiz read-only, apenas `/app/data` e `/tmp` são graváveis.
- `URNA_STATUS_ADDR` deve ser `0.0.0.0:8080` dentro do container para o
  healthcheck; a porta publicada é `127.0.0.1:8080`.
