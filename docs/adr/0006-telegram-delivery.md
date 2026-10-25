# ADR 0006 — Entrega ao Telegram

- Status: aceita
- Data: 2026-10-25

## Contexto

Notificações não podem ser perdidas em reinícios nem duplicadas trivialmente.
A API do Telegram tem limite de 4096 caracteres (unidades UTF-16), rate limit e
erros permanentes/transitórios. O token é um segredo.

## Critérios

- durabilidade antes do envio;
- IDs determinísticos/dedupe lógico;
- retry/backoff e `Retry-After`;
- split de mensagens longas;
- sanitização de erros/token;
- modo desligado sem afetar coleta;
- sem promessa de exactly-once.

## Decisão

- `sendMessage` com `parse_mode=HTML`, `disable_web_page_preview=true`.
- Mensagem dividida em chunks ≤4096 unidades UTF-16, preferindo limites de
  linha; IDs `<eventID>-<chunk>` persistidos no outbox.
- Outbox durável: criado na mesma transação do evento/snapshot; worker separado
  da coleta.
- Retry até `TELEGRAM_MAX_ATTEMPTS` com backoff exponencial + jitter; 429 usa
  `retry_after`; 5xx/rede são transitórios; 4xx é permanente.
- Sucesso só é marcado após `ok=true` da API. Falha ambígua pode duplicar;
  semântica **at-least-once**.
- Telegram desligado: eventos ficam `disabled`; pendências antigas são
  `suppressed` para não serem enviadas tardiamente.
- Token nunca é logado; erros substituem o token por `[redacted]`.

## Alternativas

- Envio síncrono sem fila: perde notificações em queda/timeout.
- Exactly-once: impossível sem transação distribuída com o Telegram.
- Markdown: HTML é mais previsível para escapar dados.
- Uma mensagem por candidato: spam; usamos resumo/limite.

## Consequências

Positivas:

- sobrevive a reinícios e rate limit;
- não bloqueia a coleta;
- dedupe por geração/mudança;
- mensagens longas suportadas.

Negativas:

- duplicata possível após falha entre envio e confirmação;
- chunks podem ser observados fora de ordem em retries;
- outbox precisa de retenção.

## Riscos

- Chat id errado envia ao grupo errado; valide antes de habilitar.
- Token revogado gera 401/403 permanente até atualização.
- HTML inválido gerado por bug vira 400; testes cobrem escaping.

## Referências

- https://core.telegram.org/bots/api#sendmessage
- https://core.telegram.org/bots/api#making-requests
