# ADR 0004 — Polling e detecção de mudanças

- Status: aceita
- Data: 2026-10-25

## Contexto

A fonte atualiza em cadência própria, pode servir 304 e pode repetir
`dt/ht`. Precisamos de polling educado, retries e eventos sem falsos positivos
por metadados/ordem de JSON.

## Critérios

- respeitar a fonte e `Retry-After`;
- cancelamento/encerramento gracioso;
- snapshots normalizados, não JSON bruto;
- separar ausência de mudança de falha;
- detectar revisão de valores publicados;
- IDs determinísticos/dedupe.

## Decisão

- Intervalo padrão **120s** (constante do SPA e observação de duas requisições
  separadas por 120s); mínimo **30s**.
- Primeira coleta imediata; jitter de ±5% no intervalo.
- Até 2 retries por ciclo com backoff curto; entre ciclos, backoff exponencial
  de 5s até 5min. `Retry-After` prevalece.
- `ETag`/`Last-Modified` persistidos; 304 atualiza sucesso e não gera evento.
- Comparação por candidato (número), votos inteiros, percentual nano, posição,
  situação, seções, totais, `dv` e `tf`.
- Mudança só de `idg`/`dg`/`hg`/`dt`/`ht` ou ordem de JSON não gera evento.
- Decremento de votos/seções/totais, remoção de candidato ou `tf` regredindo
  marcam `Revision` e geram mensagem específica.
- Snapshot mais antigo que o armazenado é rejeitado (preserva o último bom).
- Evento tem ID = hash(eleição+cargo+abrangência+versão+fingerprint da mudança).

## Alternativas

- Polling de 30s: agressivo e sem ganho (SPA usa 120s).
- Comparar JSON bruto: falsos positivos por ordem/metadados.
- Webhooks: o TSE não oferece.
- Guardar todo snapshot: volume sem necessidade (ver ADR 0005).

## Consequências

Positivas:

- carga baixa e previsível;
- eventos semanticamente estáveis;
- recuperação após falhas e revisão explícita.

Negativas:

- detecção atrasa até ~2min em relação à publicação;
- um snapshot com `dt` retrocedido é rejeitado e pode exigir nova tentativa;
- a posição pode mudar sem mudança de votos (empate/ordenação), gerando evento
  de posição.

## Riscos

- Se o TSE mudar a cadência, o padrão precisa ser revisto.
- Relógios/zonas: timestamps da fonte são interpretados em America/Sao_Paulo.

## Referências

- Código do SPA (`TEMPO_PARA_NOVA_REQUISICAO = 12e4`) e evidência de rede em
  `docs/research/tse-data-source.md`.
