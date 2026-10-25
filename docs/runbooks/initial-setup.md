# Runbook — setup inicial

## 1. Pré-requisitos

- Go 1.27+ e Make (build nativo) **ou** Docker/Compose.
- Acesso HTTPS a `resultados.tse.jus.br`.
- Grupo do Telegram e bot do BotFather (se notificar).
- Um diretório persistente para o estado.

## 2. Obter o bot e o chat id

1. BotFather → `/newbot` → token.
2. Adicione o bot ao grupo correto.
3. Envie uma mensagem no grupo.
4. Descubra o id:

   ```sh
   TOKEN='<token>'
   curl -sS "https://api.telegram.org/bot${TOKEN}/getUpdates" \
     | jq '[.result[] | (.message.chat // .my_chat_member.chat)] | unique_by(.id)'
   ```

5. Anote o id negativo. **Não** use um grupo de terceiros.

## 3. Escolher a eleição

Consulte o config oficial e confirme `cd` da eleição, cargo, turno e abrangência:

```sh
curl -sS https://resultados.tse.jus.br/oficial/comum/config/ele-c.json \
  | jq '.pl[] | {cd, c, e: [.e[] | {cd, nm, t, tp, abr: [.abr[].cd]}]}'
```

Não adivinhe códigos.

## 4. Configurar

```sh
cp .env.example .env
# edite: URNA_ELECTION/CONTEST/UF, URNA_STATE_PATH, Telegram
# NUNCA versione .env
```

Valide sem notificar:

```sh
set -a; . ./.env; set +a
./bin/urna-watch --validate --no-tui
```

## 5. Primeiro estado

Use um caminho de estado exclusivo para a seleção:

```sh
mkdir -p data && chmod 700 data
set -a; . ./.env; set +a
./bin/urna-watch --once --no-tui --no-telegram
```

Se OK, inicie com Telegram em estado temporário para um smoke test:

```sh
set -a; . ./.env; set +a
URNA_STATE_PATH=/tmp/urna-smoke/state.json timeout 15s ./bin/urna-watch --no-tui
```

Confirme uma única linha de base no grupo e `sent` no estado.

## 6. Produção

- Headless: `URNA_TUI=false`, logs JSON, `/healthz`.
- Estado em volume com backup.
- Token via secret manager/EnvironmentFile 0600.
- Habilite Telegram somente após validar a fonte e o chat.
- Monitore `/healthz`, `consecutiveFailures`, fila e idade do último sucesso.

## 7. Critérios de aceite

- [ ] `--validate` OK com endpoint impresso.
- [ ] `sourceVersion` e candidatos coerentes com a página oficial.
- [ ] Nenhum token em log/estado/repositório.
- [ ] Estado com permissão 0600.
- [ ] Linha de base entregue no grupo correto.
- [ ] `/healthz` 200 e `/status` sem segredos (se habilitado).
