# Operação

## Modos

| Modo | Comando | Rede | Persiste | Notifica |
| --- | --- | --- | --- | --- |
| TUI | `urna-watch` (TTY) | sim | sim | conforme config |
| Headless | `urna-watch --no-tui` | sim | sim | conforme config |
| Coleta única | `urna-watch --once --no-tui` | sim | não | não |
| Validação | `urna-watch --validate --no-tui` | sim | não | não |

“Coleta única” usa o estado em disco apenas para aproveitar ETag; não escreve
snapshot, eventos nem fila.

## Início

1. Escolha a eleição/cargo/abrangência no config oficial (veja
   `docs/research/tse-data-source.md`).
2. Garanta volume/diretório gravável para `URNA_STATE_PATH` (0700 no diretório,
   0600 no arquivo).
3. Configure `URNA_TUI=false` em servidor.
4. Opcional: `URNA_STATUS_ADDR=127.0.0.1:8080` e logs `json`.
5. Faça uma validação: `urna-watch --validate --no-tui`.
6. Inicie e confira o primeiro snapshot/linha de base.
7. Só então habilite o Telegram, se desejado.

## Logs

- Headless: stderr, `slog` texto/JSON.
- TUI: `data/urna-watch.log` por padrão (0600), para não corromper a tela.
- Níveis: `debug`, `info`, `warn`, `error`.
- Nunca há token; o logger redige chaves suspeitas e o cliente Telegram substitui
  o token por `[redacted]` em erros.
- Eventos relevantes: config carregado, mudança detectada, falha, retry,
  notificação enviada/falhou, mudança de intervalo, shutdown.

## Saúde

Com `URNA_STATUS_ADDR`:

```sh
curl -sS http://127.0.0.1:8080/healthz
# ok | paused | starting | no successful collection | stale collection

curl -sS http://127.0.0.1:8080/status | jq '{
  paused, error,
  poll: {lastSuccess: .poll.lastSuccess, consecutiveFailures: .poll.consecutiveFailures},
  source: {statusCode: .source.statusCode, notModified: .source.notModified},
  telegram: {enabled: .telegram.enabled, pending: .telegram.pending},
  version: .snapshot.sourceVersion,
  final: .snapshot.isFinal
}'
```

`/healthz` retorna 503 se nunca houve sucesso (após a janela inicial) ou se o
último sucesso está mais velho que `max(3×intervalo, 5min)`. Pausa é saudável.
O endpoint não expõe token, chat id, headers ou corpo bruto.

## Ciclo esperado

- Padrão 120s; o SPA oficial também atualiza a cada 120s.
- `HTTP 304` é sucesso e não gera evento.
- `dt/ht` (última totalização) pode ficar constante por longos períodos. Isso
  **não** é falha.
- Falhas transitórias geram retry curto no ciclo e backoff entre ciclos.
- `429`/`Retry-After` é respeitado; não reduza o intervalo para “compensar”.
- Após `URNA_MAX_FAILURES` ciclos ruins, uma mensagem opcional é enviada; na
  recuperação, outra (se habilitadas).

## Visão por UF (TUI)

Em uma eleição federal monitorada como Brasil (`URNA_UF=br`), pressione `u` na
TUI para escolher uma UF. O app busca sob demanda o arquivo oficial
`.../dados/<uf>/<uf>-c<cargo>-e<eleicao>-u.json` e mostra os candidatos daquela
UF. `b` volta ao consolidado.

- É somente leitura; não altera a seleção monitorada, o snapshot persistido, os
  eventos nem as notificações.
- O consolidado continua sendo coletado e notificado normalmente.
- A visão pode ficar defasada; o painel mostra quando foi carregada.
- Em eleição estadual/municipal o seletor não é oferecido (use `URNA_UF`).
- O cache em memória da visão é curto (30s) para evitar consultas repetidas.

## Mapa na TUI

Em terminais com pelo menos 100 colunas, o mapa aparece automaticamente na
tela inicial. Pressione `m` para abrir em tela cheia. O app carrega os 27
arquivos de UF (cache de 60s) e desenha o Brasil em blocos coloridos no
terminal, usando o mesmo critério do mapa do Telegram: cor do candidato com
mais votos na UF; cinza sem votos. Lula/PT é vermelho e Bolsonaro/PL é azul,
independentemente da posição. `m` ou `esc` fecha. O mapa é somente leitura e não altera o consolidado, o
estado durável ou as notificações. O `TELEGRAM_MAPS` só controla o mapa
enviado ao Telegram; o mapa da TUI não depende dele.

## Painel de disputa

Abaixo da tabela de candidatos, a TUI mostra um painel **DISPUTA — BRASIL** com:

- percentual de cada um dos dois primeiros;
- vantagem atual em pontos percentuais e em votos;
- gráfico histórico da diferença (`candidato A − candidato B`) desde a primeira
  coleta desta versão.

O histórico é limitado a 240 pontos e fica no estado JSON. Ele é atualizado
quando os dados normalizados mudam; 304/metadados não criam pontos. Em caso de
virada, as barras mudam de cor conforme o candidato que lidera no momento.
O gráfico só aparece em terminais largos; em terminais estreitos a vantagem
atual continua no painel de apuração.

## Notificações e gráficos

- A mensagem é curta: candidatos principais, mudanças de votos/seções,
  progresso e link para a página oficial.
- `TELEGRAM_CHARTS=true` (padrão) envia um PNG via `sendPhoto`, com a mensagem
  como legenda. O arquivo fica em `<diretório do estado>/media/` até o envio e
  é removido após sucesso/falha permanente; órfãos com mais de 7 dias são
  limpos no início.
- `TELEGRAM_MAPS=true` (padrão) usa o mapa do Brasil por UF vencedora no lugar
  do gráfico simples. Ele consulta os 27 arquivos de UF a cada atualização
  (máximo 4 em paralelo, cache de 60s e ETag); se alguma UF falhar, usa o cache
  anterior ou mostra cinza, sem interromper a coleta/notificação.
- O mapa pode ficar alguns segundos/minutos atrás do consolidado; o caption
  mostra a atualização do consolidado.
- Se não houver votos (início da apuração) ou a legenda passar de 1024
  unidades UTF-16, o app envia texto normal.
- `TELEGRAM_CHARTS=false` mantém apenas texto.
- O link do TSE é a página oficial; o JSON não é enviado a humanos.

## Snapshot e eventos

- O último snapshot válido nunca é substituído por resposta inválida/mais
  antiga.
- Eventos são deduplicados por ID determinístico (eleição + geração + mudança).
- Linha de base: uma mensagem, sem despejar histórico.
- Revisões (valores diminuindo, candidato removido, `tf` voltando a `n`) são
  explicitamente rotuladas.
- Histórico de eventos é limitado por `URNA_RETENTION_EVENTS`; snapshot é
  apenas o último.

## Outbox e reinícios

- Notificações são persistidas antes de serem enviadas.
- Reinício retoma pendentes e respeita a próxima tentativa.
- `sent` é gravado após confirmação da API. Se o processo cair entre o envio e
  a gravação, pode haver **duplicata**; a semântica é at-least-once.
- Se o Telegram for desligado, pendências antigas são suprimidas para evitar
  envio tardio inesperado.
- Falha permanente (4xx) é marcada `failed`; transitória (rede/5xx/429) tenta
  até `TELEGRAM_MAX_ATTEMPTS`, com backoff.

## Backup e recuperação

O estado contém dados públicos e não contém token. Ainda assim:

```sh
# parar o serviço antes de copiar (um escritor por arquivo)
cp -a data/state.json "backup/state-$(date -u +%Y%m%dT%H%M%SZ).json"
```

- `fsync` + `rename` garantem substituição atômica; não copie enquanto escreve
  se puder parar.
- Se o JSON estiver corrompido, o app falha com mensagem clara. Faça backup do
  arquivo corrompido e remova-o para começar de novo (perde dedupe/histórico).
- Migração de versão é explícita (`version`); não edite manualmente sem
  necessidade.

## Retenção

- Eventos: `URNA_RETENTION_EVENTS` (20–5000).
- Outbox: enviados >7 dias, falhos/suprimidos >30 dias (limpeza horária).
- Estado total é pequeno (um snapshot + eventos + fila); monitore o tamanho do
  volume se usar cargos proporcionais com muitos candidatos.

## Atualização de versão

```sh
make check
make docker-build
# pare, faça backup do estado, suba a nova imagem
docker compose down
docker compose up -d --build
```

O estado declara `version`. Uma versão futura do app deve recusar versões mais
novas do que entende; faça backup antes.

## Docker/Compose

```sh
docker compose up -d --build
docker compose ps
docker compose logs -f urna-watch
curl -sS http://127.0.0.1:8080/healthz
docker compose down
```

- Container non-root (`urna`, UID 100/GID 101), raiz read-only,
  `no-new-privileges`, `/tmp` tmpfs, volume nomeado `urna-data` (inicializado
  com a posse correta a partir de `/app/data`). Para bind mount, use
  `mkdir -p data && sudo chown -R 100:101 data`.
- `restart: unless-stopped`, `stop_grace_period: 30s`.
- Healthcheck usa `/healthz`.
- `env_file` é opcional no Compose; exporte as variáveis ou crie `.env` local.
- Backup do volume nomeado:

  ```sh
  docker compose stop urna-watch
  docker run --rm -v tse-watch_urna-data:/data -v "$PWD/backup":/backup \
    alpine:3.22 cp /data/state.json /backup/state-$(date -u +%Y%m%dT%H%M%SZ).json
  docker compose start urna-watch
  ```

  O prefixo do volume é o nome do projeto Compose (`tse-watch` no checkout
  original). Confira com `docker volume ls`.

## systemd (exemplo)

```ini
[Unit]
Description=Urna Watch
After=network-online.target
Wants=network-online.target

[Service]
User=urna
Group=urna
WorkingDirectory=/opt/urna-watch
EnvironmentFile=/etc/urna-watch.env
ExecStart=/opt/urna-watch/urna-watch --no-tui
Restart=on-failure
RestartSec=10
TimeoutStopSec=30
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/urna-watch
StateDirectory=urna-watch
# URNA_STATE_PATH=/var/lib/urna-watch/state.json no EnvironmentFile

[Install]
WantedBy=multi-user.target
```

`EnvironmentFile` deve ter permissão restrita (0400/0600) por conter o token.

## Rotação do token

1. BotFather → `/mybots` → bot → API Token → revoke.
2. Gere novo token.
3. Atualize o secret (não o repositório).
4. Reinicie o serviço.
5. Confirme `getMe` e uma notificação de teste/linha de base em estado novo.

Token exposto em chat, log, screenshot ou commit = revogar imediatamente.

## Checklist de incidente

- [ ] `/healthz` e `/status` (se habilitado)
- [ ] logs sem token (`grep` por `bot[0-9]` deve retornar só `[redacted]`)
- [ ] último sucesso e `consecutiveFailures`
- [ ] código HTTP/`Retry-After` da fonte
- [ ] conectividade HTTPS e horário (America/Sao_Paulo)
- [ ] fila do Telegram e último erro
- [ ] disco/permissões do estado
- [ ] se a fonte mudou: abrir a página oficial e refazer a investigação
