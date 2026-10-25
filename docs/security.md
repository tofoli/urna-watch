# Segurança

## Segredos

- O único segredo do projeto é o **token do bot do Telegram**.
- O token entra por `TELEGRAM_BOT_TOKEN` (ambiente/secret manager) ou
  `telegram.botToken` em arquivo local ignorado.
- `.env` é ignorado pelo Git; `.env.example` contém apenas placeholders.
- O token **não** aparece em:
  - logs (logger redige chaves com `token`/`authorization`/`secret`);
  - erros do cliente Telegram (substituído por `[redacted]`);
  - estado persistido;
  - TUI;
  - mensagens enviadas;
  - build/Docker (nenhum `ARG`/`ENV` com token).
- O chat id não é um token, mas também não é persistido; trate-o como
  configuração operacional.
- Token exposto (chat, commit, print, histórico de shell) deve ser **revogado**
  no BotFather e substituído.

## Fonte externa não confiável

- O JSON do TSE é tratado como dado não confiável: tamanho máximo de 32 MiB,
  parsing estrito, validação de campos e invariantes.
- Nomes/partidos/status são escapados para HTML antes de ir ao Telegram.
- Dados externos nunca são interpolados em comandos, SQL ou paths.
- Não há HTML no pipeline de coleta.
- URLs são construídas a partir de códigos validados e base configurada; não há
  redirect automático para hosts arbitrários (o cliente segue redirects padrão
  do servidor, mas o conteúdo continua sujeito ao parser/limite).
- Erros de rede são sanitizados; o cliente não despeja cabeçalhos.

## Transporte

- Padrão HTTPS para TSE e Telegram; `--once`/`--validate` também.
- HTTP é aceito apenas para conveniência de testes/espelhos locais. Não use em
  produção.
- Timeouts: requisição, header, idle; conexões são reutilizadas e limitadas por
  host.
- `Retry-After` e backoff evitam abuso/rate limit.

## Persistência

- Estado em arquivo `0600`, diretório `0700`.
- Escrita atômica (`CreateTemp` + `chmod` + `write` + `fsync` + `rename` +
  `fsync` do diretório).
- Um escritor por arquivo; não compartilhe o mesmo estado entre instâncias.
- Estado não contém token nem chat id. Ainda assim, faça backup com acesso
  restrito.
- Corrupção falha alto em vez de resetar silenciosamente.

## Container

- Build multi-stage; runtime Alpine mínimo.
- Usuário non-root `urna`.
- `read_only: true`, volume apenas em `/app/data`, `/tmp` tmpfs.
- `no-new-privileges:true`.
- Sem capabilities adicionais, sem docker.sock.
- `ca-certificates` e `tzdata` presentes (HTTPS + America/Sao_Paulo).
- Healthcheck não expõe segredos; `/status` não inclui token/chat id.

## Notificações

- Formato HTML fixo; texto externo escapado.
- Limite de 4096 unidades UTF-16 para texto e 1024 para legenda de foto;
  split em fronteiras de linha/runa quando necessário.
- Gráficos são PNGs gerados localmente a partir do snapshot; nenhum dado
  externo é executado. Arquivos temporários ficam em `media/` com `0600`.
- Outbox deduplica por evento/chunk.
- Semântica at-least-once: uma falha ambígua pode duplicar uma mensagem; isso é
  preferível a perder notificação.
- Falha permanente não entra em loop infinito; fica registrada para inspeção.

## Dependências

- Diretas: Bubble Tea, Lip Gloss, `x/ansi` (mais dependências transitivas do
  ecossistema Charm).
- Antes de adicionar: licença, manutenção, tamanho, CGO e necessidade real.
- `govulncheck` roda no CI.
- Não há banco nativo/CGO, o que reduz superfície de build.

## Operação

- Rode com o menor privilégio possível.
- Restrinja o acesso a `/status` (bind em localhost/rede interna).
- Monitore `x-ratelimit-*` apenas como informação; não aumente frequência.
- Rotacione o token periodicamente e após qualquer suspeita.
- Mantenha o host/container atualizado e o relógio sincronizado.
- Em caso de incidente, preserve logs e estado, mas remova qualquer token antes
  de compartilhá-los.

## O que este projeto não faz

- Não verifica assinatura JWS do TSE (ver ADR 0003).
- Não autentica a fonte além de TLS/HTTPS.
- Não promete exactly-once no Telegram.
- Não é afiliado ao TSE nem substitui a apuração oficial.
