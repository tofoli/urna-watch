# Runbook — monitoramento de uma eleição

## Antes de abrir

- [ ] `--validate` OK e endpoint confere com a página oficial.
- [ ] Estado exclusivo e com backup.
- [ ] Telegram no grupo correto (smoke test feito).
- [ ] Intervalo ≥120s (não reduzir durante apuração).
- [ ] Relógio/timezone do host corretos.
- [ ] `/healthz` (se habilitado) responde 200.

## Durante

Acompanhe:

- **Idade do último sucesso**: deve ficar ≤2×intervalo. Acima disso, investigue.
- **`consecutiveFailures`**: 1–2 é transitório; ≥3 dispara alerta e backoff.
- **`source.statusCode`**: 200/304 normal; 404 = arquivo/escopo errado ou ainda
  não publicado; 429 = pare e respeite `Retry-After`; 5xx = retry.
- **`snapshot.sourceVersion`/`TotalizedAt`**: versão nova com `dt` igual pode
  significar metadados; não é falha.
- **`snapshot.isFinal`**: só fica true com `tf=s`.
- **Fila do Telegram**: `pending` deve voltar a 0; `failed` exige ação.
- **Revisões**: mensagens “Revisão de dados publicados” são esperadas se a fonte
  corrigir valores; confira na página antes de comunicar.

## O que não fazer

- Não reiniciar em loop para “forçar” atualização.
- Não reduzir o intervalo por ansiedade; o SPA atualiza a cada 120s.
- Não apagar o estado para “limpar” falhas sem antes copiar o arquivo.
- Não publicar conclusão baseada em `pst=100`; use `isFinal`.

## Trocar de eleição/cargo/abrangência

1. Pare a instância atual.
2. Faça backup do estado.
3. Crie um **novo** `URNA_STATE_PATH` (o app recusa estado de outra seleção).
4. Ajuste election/contest/UF/município.
5. Rode `--validate --no-tui`.
6. Inicie; a primeira coleta cria nova linha de base.

Não edite o JSON do estado para forçar a troca.

## Encerramento

`SIGINT`/`SIGTERM` → poller para, worker termina o envio em andamento, estado é
consistente. Em Docker, `stop_grace_period: 30s`. Não use `kill -9` se puder
evitar.

## Pós-apuração

- `tf=s` e mensagem de totalização final.
- Mantenha o processo por um período de segurança (a fonte pode publicar
  revisões).
- Faça backup final do estado.
- Se não houver mais necessidade, pare a instância e revogue/rotacione o token
  se o bot não for mais usado.
