# ADR 0003 — Fonte de dados do TSE

- Status: aceita (com riscos; fonte sem contrato público)
- Data: 2026-10-25
- Evidência: [`docs/research/tse-data-source.md`](../research/tse-data-source.md)

## Contexto e problema

É preciso obter resultados oficiais, estruturados, de forma eficiente, sem
inventar endpoints e sem scraping de HTML. O TSE publica um SPA que consome
arquivos JSON; não localizamos um contrato de API público/versionado.

## Critérios

- dado oficial e verificável;
- estrutura JSON, não HTML;
- identificação de eleição/cargo/abrangência;
- possibilidade de consulta condicional (ETag) e baixa frequência;
- conclusão oficial (`tf`), não inferida;
- rastreabilidade da investigação.

## Alternativas

- **Scraping do SPA/HTML**: descartado (frágil, caro, proibido no escopo).
- **API interna do SPA (`.jws`)**: viável, mas exige validar assinatura; o
  payload é o mesmo dos `.json`.
- **Arquivos `.json` oficiais**: escolhidos.
- **Dados de terceiros**: descartado (não é fonte primária).

## Decisão

Consumir os arquivos JSON estáticos usados pelo SPA oficial:

```text
/oficial/comum/config/ele-c.json
/oficial/<ciclo>/<eleicao>/dados/<abr>/<arquivo>-c<cargo4>-e<eleicao6>-u.json
```

- `uf=br` para agregado nacional; UF para recortes estaduais; município no
  formato `<uf><municipio5>`.
- Consulta condicional com `If-None-Match` e `If-Modified-Since`; 304 é sucesso.
- `tf=s` é a única marca de totalização final.
- Validar estruturalmente; 404/JSON inválido não viram snapshot vazio.
- Polling padrão 120s (cadência do SPA), mínimo 30s.
- Não validar JWS no MVP; usar `.json`. Se a fonte exigir `.jws`, migrar e
  implementar verificação EdDSA.

## Consequências

Positivas:

- dado primário, reproduzível e barato;
- sem HTML, sem navegador em produção;
- ETag reduz tráfego e permite detectar ausência de mudança;
- evidências permitem auditoria.

Negativas/riscos:

- caminhos e campos podem mudar sem aviso;
- cache/CDN pode entregar geração anterior (mitigado por comparação de
  `TotalizedAt` e retry);
- `.json` não é assinado; integridade depende de TLS/CDN;
- o formato EA20 não é documentado como contrato público;
- 2024 não foi validado (404 nos caminhos testados em 2026-10-25).

## Revisão

Reabrir esta ADR se:

- o TSE publicar documentação/contrato;
- `.json` deixar de existir ou divergir do `.jws`;
- o esquema/URL mudar;
- for necessário suportar eleições antigas.

## Referências

- https://resultados.tse.jus.br/oficial/app/index.html
- https://www.tse.jus.br/eleicoes/eleicoes-2026
- Pesquisa com Browser Harness, `curl` e fixtures em 2026-10-25.
