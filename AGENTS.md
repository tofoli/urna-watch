# AGENTS.md — instruções para agentes de codificação

Este arquivo é a fonte primária para agentes que alteram o repositório. Leia
também `README.md`, `docs/architecture.md`, `docs/research/tse-data-source.md`
e os ADRs antes de mudanças estruturais.

## 1. Objetivo e escopo

Urna Watch monitora resultados oficiais do TSE (arquivos JSON do portal de
resultados), detecta mudanças normalizadas, persiste o último estado válido e
notifica um grupo do Telegram. A TUI é uma camada de apresentação; a coleta
roda sem ela.

Fora de escopo no MVP: scraping de HTML, previsão/projeção de resultados,
cálculo independente de votos, múltiplas eleições na mesma instância, releases
automáticas e verificação de assinatura JWS.

## 2. Arquitetura e responsabilidades

```text
cmd/urna-watch        flags, sinais, logger, modos once/validate/TUI/headless
internal/config       defaults, JSON estrito, env, validação
internal/tse          adapter HTTP + parser do formato EA20 do TSE
internal/domain       modelo imutável, medidas opcionais, ChangeSet, EventRecord
internal/detector     comparação normalizada e IDs determinísticos
internal/polling      agenda, retries, backoff, pause/resume, métricas
internal/store        estado JSON atômico, eventos, outbox, retenção
internal/notification formatação pt-BR e worker do outbox
internal/telegram     Bot API sendMessage, split UTF-16, erros sanitizados
internal/tui          Bubble Tea/Lip Gloss, mouse, detalhes, layout responsivo
internal/observability logger estruturado, Status e servidor /healthz,/status
internal/app          wiring, ciclo de coleta, baseline, falhas, persistência
```

Fluxo principal:

```text
Fetch (ETag) → Parse → Validate → Normalize → Detect → Persist → Enqueue → Telegram
```

Regras de dependência:

- `domain` não importa nada do projeto.
- `tse` não importa `store`, `telegram` nem `tui`.
- `tui` só lê `observability.Status` e chama `Pause/Resume/Refresh/SelectUF/LoadMap`
  via interface.
- A coleta nunca depende da TUI.
- Evite abstrações genéricas; interfaces pequenas e no ponto de uso.

## 3. Comandos

```sh
make build          # bin/urna-watch com ldflags
make test           # go test ./... (offline)
make test-race      # go test -race ./...
make lint           # go vet + golangci-lint se instalado
make fmt            # gofmt -w cmd internal tests
make fmt-check      # falha se gofmt for necessário
make check          # fmt-check + lint + test + build
make run            # execução interativa
make once           # coleta única headless
make validate       # valida fonte oficial
make docker-build
make docker-up
```

Go: **1.27+**. Dependências: apenas Bubble Tea, Lip Gloss e `x/ansi` diretas.
Antes de adicionar dependência, verifique licença, manutenção, tamanho e se a
biblioteca padrão já resolve. Rode `go mod tidy` e justifique em ADR se for
estrutural.

Sem credenciais nos testes. A suíte padrão não pode depender de TSE/Telegram.

## 4. Convenções de código

- Go idiomático; erros com `%w`; `context.Context` como primeiro parâmetro.
- Não adicione comentários óbvios. Comente decisões e invariantes.
- Não use `float64` para votos/percentuais; use inteiros (`Measure`,
  `PercentNano`).
- Campos ausentes ≠ zero: use `Measure{Known:false}`.
- Tempo da fonte é America/Sao_Paulo; persista `time.Time` e exiba local.
- Nomes de domínio em inglês; mensagens de usuário em pt-BR.
- Logs estruturados (`slog`), sem tokens, cabeçalhos sensíveis ou HTML externo.
- Nunca interpole dados externos em comandos executáveis.
- Estado: escrita atômica, 0600, um escritor por arquivo.
- Não use `panic` para erros operacionais.
- Mudanças de comportamento exigem teste e atualização de documentação.

## 5. Estratégia de testes

Obrigatório cobrir, quando aplicável:

- parsing de fixtures reais e respostas inválidas/incompletas;
- ausência de falso positivo por ordem de JSON/metadados;
- mudanças de votos, percentual, seções, totais, posição, situação e final;
- revisão (valores diminuindo);
- 304, 429 com `Retry-After`, 5xx, timeout e cancelamento;
- persistência, recuperação e corrupção de estado;
- deduplicação, retry, 429 e divisão de mensagens do Telegram;
- TUI em múltiplas dimensões, teclado, mouse e modais;
- pipeline integrado com `httptest`.

Fixtures ficam em `tests/fixtures/tse/` com origem/data/hash. Não edite uma
fixture real para simular mudança: gere a variação em memória no teste. Se
adicionar fixture, atualize o README e o `docs/research/tse-data-source.md`.

## 6. Fonte TSE — como investigar e alterar

Antes de mudar o adapter:

1. Leia `docs/research/tse-data-source.md` e a ADR 0003.
2. Use Browser Harness para abrir a página oficial e inspecionar XHR/fetch.
3. Confirme `.json` e `.jws`, headers, 304, 429 e o payload.
4. Reproduza com `curl` e compare valores visíveis na página.
5. Registre URL, data, comando, exemplo sanitizado e limitações.
6. Atualize `internal/tse/parse.go`, fixtures e testes.

Proibições:

- inventar endpoint, parâmetro, campo ou semântica;
- declarar conclusão por `pst=100` quando `tf` não é `s`;
- transformar 404/JSON inválido em snapshot vazio;
- fazer polling agressivo (mínimo 30s; padrão 120s);
- baixar/parsear HTML em produção.

Campos de conclusão: `tf=s`. `dv=s` é divulgação de votação. `idg` identifica a
geração. `dt/ht` é a última totalização (pode ficar constante). `st="2º turno"`
não significa eleito.

## 7. Notificações

- Mensagens seguem o formato de `internal/notification/format.go`.
- Todos os valores vêm do snapshot; nada é inventado.
- Escape HTML em todo texto vindo da fonte.
- Linha de base é uma mensagem (configurável, padrão ligada).
- Deduplicação lógica por `ChangeSet.EventID()`; outbox com IDs
  `<eventID>-<chunk>`.
- Semântica **at-least-once**; não prometa exactly-once.
- Limite Telegram: 4096 unidades UTF-16 para texto e 1024 para legenda de
  foto; use `telegram.SplitMessage`/`SendPhoto`.
- `TELEGRAM_CHARTS` gera PNG; `TELEGRAM_MAPS` usa a malha oficial do IBGE e os
  27 arquivos de UF para colorir o mapa. O mapa é derivado dos totais oficiais,
  não é projeção; UFs sem dados ficam cinza.
- O link da mensagem é sempre a página humana do TSE, nunca o JSON.
- Ao mudar o formato, adicione/ajuste testes de conteúdo, divisão, foto e mapa.
- Nunca inclua token em texto, log, erro, mídia ou estado.

## 8. Persistência

Estado JSON é decisão aceita (ADR 0005). Se propuser SQLite, justifique com
necessidade real (consultas, concorrência, volume) e crie ADR substituta.
Sempre:

- escrita atômica e `fsync`;
- retenção de eventos limitada;
- dedupe de eventos/notificações;
- não sobrescrever snapshot bom com inválido/mais antigo;
- migração de versão explícita (`StateVersion`);
- backup documentado.

## 9. ADRs e documentação

- ADRs em `docs/adr/NNNN-titulo.md`, formato MADR, status explícito.
- Mudanças arquiteturais futuras: nova ADR; não reescreva a história.
- O ADR da fonte deve permanecer **proposed** se houver investigação pendente.
- Atualize `README`, `docs/*`, `CHANGELOG` e `AGENTS.md` no mesmo commit da
  mudança que os torna obsoletos.
- Não crie arquivos/diretórios vazios.

## 10. Segurança e operação

- Nunca versione `.env`, token, chat id privado ou estado real.
- Logs não podem conter `TELEGRAM_BOT_TOKEN`; o logger redige chaves suspeitas
  e o cliente Telegram substitui o token por `[redacted]`.
- Container non-root, filesystem raiz read-only, `no-new-privileges`.
- Timeouts, limites de corpo (32 MiB), conexões reutilizadas e encerramento
  gracioso são obrigatórios.
- Token exposto = revogar no BotFather e gerar outro.
- Estado contém dados públicos e chat id? O chat id não é persistido; mesmo
  assim, trate o arquivo como operacional e faça backup.

## 11. Evidências e honestidade

- Nunca declare teste aprovado sem executar o comando.
- Nunca invente saída de teste, endpoint, captura ou mensagem enviada.
- Se não for possível validar, registre a limitação e mantenha a decisão como
  pendente/proposta.
- Em relatórios, separe “executado” de “planejado” e “não verificado”.
