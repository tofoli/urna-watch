# Runbook — resposta a incidentes

## Segurança: token exposto

1. Revogue o token no BotFather (`/mybots` → bot → API Token → Revoke).
2. Gere um novo.
3. Atualize o secret; reinicie.
4. Verifique logs/estado/repositório por vazamento (`grep` por `bot[0-9]`).
5. Se houve commit, remova do histórico e considere o token comprometido mesmo
   após remoção.
6. Registre o incidente e a rotação.

## Coleta falhando (≥3 ciclos)

1. `curl -I` no endpoint impresso por `--validate`.
2. Verifique `/status` (`source.statusCode`, `LastError`) e logs.
3. 404: confirme códigos no config/oficial e se o arquivo existe.
4. 429: respeite `Retry-After`; não reinicie para burlar.
5. 5xx/timeout/DNS: aguarde backoff; verifique CA/proxy/relógio.
6. JSON inválido: salve o corpo, compare com fixture, abra investigação; não
   relaxe validação sem evidência.
7. Snapshot “mais antigo”: CDN pode estar defasada; aguarde retry. O último bom
   foi preservado.
8. Se a página oficial também não atualiza, é ausência de publicação, não falha
   do coletor.

## Notificações não chegam

1. `TELEGRAM_ENABLED`, token e chat id corretos?
2. `getMe` OK? `sendMessage` de teste OK?
3. Bot removido/sem permissão → 400/403 permanente.
4. 429 → aguarde; worker respeita `retry_after`.
5. Veja `queue[].state/lastError` no estado (sem token).
6. Se desligou o Telegram com fila, as pendências foram suprimidas; a próxima
   mudança gera nova mensagem.
7. Não recrie o estado para “reenviar tudo” sem necessidade.

## Estado corrompido

1. Pare o processo.
2. Copie `state.json` para backup.
3. Se necessário reiniciar do zero: renomeie/remova e reinicie; a linha de base
   será recriada.
4. Nunca edite manualmente sem entender `version` e dedupe.

## Container unhealthy

- `docker compose logs --tail=200`.
- `curl` interno ao `/healthz`; verifique `URNA_STATUS_ADDR=0.0.0.0:8080`.
- Volume `urna-data`/bind mount gravável pelo UID 100 (usuário `urna`)?
- Raiz read-only: só `/app/data` e `/tmp` são graváveis.
- Token inválido não derruba a coleta, mas falha notificações.

## Suspeita de dados errados

1. Abra a página oficial da mesma eleição/cargo/abrangência.
2. Compare candidato/votos/percentual e `dt/ht`.
3. Rode `--once --json` e compare `sourceVersion`.
4. Se divergir, não “corrija” manualmente: registre URL/data/hora, corpo e
   hashes, e abra uma investigação na fonte.
5. Revisões legítimas são notificadas; não suprima sem analisar.

## Encerramento de emergência

- Prefira `SIGTERM` (graceful).
- Se precisar de `SIGKILL`, verifique depois o estado e a fila; pode haver
  duplicata de notificação (at-least-once).

## Comunicação

- Informe o que foi **verificado** vs. **suspeita**.
- Nunca cole token/chat id em canais públicos.
- Anexe logs sanitizados e o `/status` sem segredos.
