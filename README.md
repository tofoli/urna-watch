# Urna Watch

Monitor dos **resultados oficiais** das eleições brasileiras publicados pelo
Tribunal Superior Eleitoral (TSE). Coleta arquivos JSON oficiais, normaliza os
dados, detecta alterações relevantes, persiste o último estado válido e envia
notificações para um grupo do Telegram. A operação principal acontece em uma
TUI (Bubble Tea + Lip Gloss) com teclado e mouse; o mesmo binário roda sem TUI
em servidores e containers.

> Projeto independente, não oficial. A fonte é o portal de resultados do TSE.
> O TSE não publica um contrato de API estável; leia
> [`docs/research/tse-data-source.md`](docs/research/tse-data-source.md) antes
> de operar em produção.

## O que já foi validado

- Fonte oficial 2026: config comum, 1º turno, 2º turno federal, 2º turno
  estadual (AM) e um resultado municipal suplementar (Narandiba/SP).
- Requisições reais via Browser Harness e reprodução por `curl`; os valores da
  página oficial conferem com os arquivos usados pelo coletor.
- `ETag`/`Last-Modified`, `304`, cache HTTP, cabeçalhos de rate limit e o
  intervalo de atualização do SPA (120s).
- Testes automatizados com fixtures reais e servidores HTTP locais.
- Envio real de mensagem para um grupo do Telegram em 2026-10-25.

## Princípios

- Dados oficiais e estruturados; **não** há scraping de HTML na coleta.
- Nenhum endpoint ou campo é inventado; evidências ficam em `docs/research/`.
- O último snapshot válido nunca é sobrescrito por resposta inválida ou mais
  antiga.
- A linha de base evita enxurrada de mensagens históricas.
- Notificações têm IDs determinísticos e outbox durável; a semântica é
  **at-least-once** (pode haver duplicata após falha ambígua entre envio e
  confirmação), com deduplicação lógica.
- `tf=s` é a única indicação de totalização final. `pst=100` sozinho não é
  tratado como “apuração concluída”.
- Percentuais exatos são inteiros (nano-percentual); comparações não usam
  `float`.
- Tokens nunca vão para logs, TUI, estado ou repositório.

## Requisitos

- Go **1.27+** (o módulo declara `go 1.27.3`).
- `make`, `git`.
- Opcional: Docker/Compose, `golangci-lint` 2.x.
- Acesso HTTPS a `resultados.tse.jus.br` e (se habilitado) `api.telegram.org`.

## Início rápido

```sh
cp .env.example .env       # ajuste; NÃO versione .env
make check                 # gofmt + vet + testes + build
make build
./bin/urna-watch --validate --no-tui
```

Execução interativa (padrão 2º turno presidencial de 2026):

```sh
# O binário lê variáveis exportadas no shell; .env não é carregado sozinho.
export URNA_ELECTION=6258 URNA_CONTEST=1 URNA_UF=br
make run
```

Execução headless (servidor):

```sh
URNA_TUI=false ./bin/urna-watch --no-tui
```

Coleta única, sem persistir nem notificar:

```sh
./bin/urna-watch --once --no-tui --no-telegram
./bin/urna-watch --once --json --no-tui --no-telegram | jq .
```

Validação da fonte e do normalizador:

```sh
./bin/urna-watch --validate --no-tui
```

## Configuração

Principais variáveis (lista completa em [`docs/configuration.md`](docs/configuration.md)):

| Variável | Padrão | Descrição |
| --- | --- | --- |
| `URNA_ELECTION` | `6258` | código da eleição no config oficial |
| `URNA_CONTEST` | `1` | código do cargo (`--cargo` é alias) |
| `URNA_UF` | `br` | `br` ou UF (`sp`, `am`, ...) |
| `URNA_MUNICIPALITY` | vazio | código de 5 dígitos; exige UF |
| `URNA_POLL_INTERVAL` | `120s` | mínimo 30s |
| `URNA_STATE_PATH` | `./data/state.json` | estado durável (uma eleição por arquivo) |
| `URNA_TUI` | `true` | desligado automaticamente se stdout não for TTY |
| `URNA_STATUS_ADDR` | vazio | ex.: `127.0.0.1:8080` para `/healthz` e `/status` |
| `TELEGRAM_ENABLED` | `false` | habilita envio |
| `TELEGRAM_BOT_TOKEN` | — | segredo do BotFather |
| `TELEGRAM_CHAT_ID` | — | id do grupo (negativo) |

Configuração opcional por JSON (validada com campos estritos):

```sh
./bin/urna-watch --config config.example.json
```

Precedência: padrões < arquivo JSON < variáveis de ambiente < flags.

## Telegram

1. Crie um bot com o **@BotFather** e copie o token.
2. Crie/adicionar o bot ao grupo. O bot precisa poder enviar mensagens; para
   receber comandos ele não depende de privacy mode, mas o envio é o essencial.
3. Envie qualquer mensagem no grupo e descubra o `chat.id`:

   ```sh
   TOKEN='<seu-token>'
   curl -sS "https://api.telegram.org/bot${TOKEN}/getUpdates" \
     | jq '[.result[] | (.message.chat // .my_chat_member.chat) | select(. != null)] | unique_by(.id)'
   ```

   O id de grupo é negativo (ex.: `-100...` ou `-5...`). Use **apenas** o grupo
   desejado.
4. Exporte `TELEGRAM_ENABLED=true`, `TELEGRAM_BOT_TOKEN` e `TELEGRAM_CHAT_ID`.
5. Inicie o monitor. A primeira coleta envia uma **linha de base** (uma única
   mensagem) se `TELEGRAM_BASELINE=true`.

O bot **não** consegue ler mensagens do grupo; ele só envia. O token foi
projetado para ser secreto: se ele aparecer em logs, histórico ou captura de
tela, revogue e gere outro no BotFather.

```sh
# teste de fumaça do token (não envia mensagem)
curl -sS "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getMe" | jq .
```

## TUI

```text
URNA WATCH · acompanhamento oficial
Presidente · Brasil · 2º turno · fonte: TSE
OK · coleta há 12s · próxima há 1m48s · HTTP 304 · Telegram em dia
```

- `↑/↓` ou `j/k`: selecionar/rolar
- `tab`: alternar candidatos ↔ eventos
- `enter`: detalhes do candidato/evento
- `u`: abrir a **visão por UF** (eleição federal)
- `b`: voltar ao consolidado Brasil
- `p`: pausar/retomar polling
- `r`: atualização imediata
- `h`/`?`: ajuda
- `q`: sair
- Mouse: clique nas linhas e use a roda para rolar.

A visão por UF é carregada sob demanda do arquivo oficial da UF, é somente
leitura e **não** altera a linha de base, os eventos nem as notificações do
consolidado Brasil. Ela pode ficar defasada em relação ao consolidado; o painel
mostra o horário da carga e a fonte.

A coleta, a persistência e o Telegram continuam em goroutines próprias; a TUI
apenas lê um snapshot imutável. Em terminal sem TTY, o modo headless é usado.

## Docker

```sh
cp .env.example .env   # opcional; sem .env o compose usa os padrões
docker compose up -d --build
docker compose logs -f
curl -sS http://127.0.0.1:8080/healthz
curl -sS http://127.0.0.1:8080/status | jq '{paused, poll, telegram, snapshot: .snapshot.sourceVersion}'
docker compose down
```

O container roda como usuário sem privilégios, com filesystem raiz somente
leitura e volume nomeado `urna-data` (o diretório da imagem já pertence ao
usuário `urna`, então o volume inicializa gravável). O token entra por variável
de ambiente; nunca é embutido na imagem. Para usar bind mount, crie o diretório
e ajuste a posse para o UID/GID do container (`100:101`).

## Persistência e entrega

- Estado em JSON com escrita atômica (`tmp` + `fsync` + `rename`), permissão
  `0600`.
- Inclui último snapshot válido, validadores HTTP, eventos recentes e outbox de
  notificações.
- Se o Telegram estiver desabilitado, notificações pendentes antigas são
  marcadas como suprimidas (não são enviadas ao reativar).
- Reinícios retomam a fila pendente; deduplicação por ID determinístico evita
  reenvio da mesma geração.
- Uma única instância deve escrever em cada `URNA_STATE_PATH`.
- O estado pertence a uma seleção (eleição/cargo/abrangência); trocar de
  eleição exige outro caminho de estado.

## Arquitetura (resumo)

```text
TSE JSON → Adapter → Normalizer/Validator → Detector → Store (snapshot + eventos + outbox)
                                                           ↓
TUI / Status HTTP ← App ← Poller (retry/backoff/304)       Telegram Adapter
```

Componentes em `internal/`: `tse`, `domain`, `detector`, `polling`, `store`,
`notification`, `telegram`, `tui`, `observability`, `app`, `config`.
Detalhes em [`docs/architecture.md`](docs/architecture.md) e ADRs em
[`docs/adr/`](docs/adr/).

## Desenvolvimento

```sh
make fmt-check
make lint          # go vet + golangci-lint quando instalado
make test
make test-race
make check
```

Testes padrão não acessam TSE nem Telegram; usam fixtures e `httptest`.
`go test ./...` cobre parser, config, detector, store, polling, Telegram, TUI e
um pipeline completo com servidores simulados.

## Documentação

- [Pesquisa da fonte](docs/research/tse-data-source.md)
- [Arquitetura](docs/architecture.md) · [ADRs](docs/adr/README.md)
- [Configuração](docs/configuration.md) · [Desenvolvimento](docs/development.md)
- [Operação](docs/operations.md) · [Troubleshooting](docs/troubleshooting.md)
- [Segurança](docs/security.md) · [Testes](docs/testing.md)
- Runbooks: [setup](docs/runbooks/initial-setup.md),
  [monitoramento](docs/runbooks/election-monitoring.md),
  [incidentes](docs/runbooks/incident-response.md)

## Limitações conhecidas

- Fonte sem contrato público; caminhos podem mudar.
- `.json` escolhido em vez de validar JWS (ver ADR 0003).
- Apenas dados de 2026 foram validados; 2024 retornou 404 nos caminhos testados.
- O MVP não alterna eleições em tempo de execução; use outra instância/estado.
- A visão por UF é por demanda e não é persistida; ela não gera notificações.
- A variação “desde o snapshot anterior” some após reinício (não é persistida
  como histórico de deltas; o snapshot e os eventos são).
- Município: URL validada, mas a coerência código↔UF é delegada ao 404 da fonte.
- Não há release/publicação automática; versionamento é manual.

## Segurança

Leia [`docs/security.md`](docs/security.md). Em resumo: não versione `.env`,
nunca registre tokens, trate JSON externo como não confiável, use HTTPS,
mantenha o container sem privilégios e faça backup do estado antes de mudanças.

## Licença

MIT — veja [LICENSE](LICENSE).
