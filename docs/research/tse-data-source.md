# Fonte de dados oficial do TSE — investigação

Status: **validada em 2026-10-25** para a eleição de 2026 (1º e 2º turnos) e para
um resultado municipal suplementar de 2026. A fonte **não é uma API pública
documentada e versionada**; são arquivos JSON estáticos consumidos pelo próprio
SPA oficial de resultados. Mudanças futuras são possíveis.

## 1. Página investigada

Página fornecida e aberta com Browser Harness (Chrome/CDP):

```text
https://resultados.tse.jus.br/oficial/app/index.html#/eleicao/6258/uf/br/cargo/1/vis/nominal/resultados
```

Contexto observado: Eleição Ordinária Federal 2026, 2º turno, Presidente,
abrangência Brasil. A página exibiu, no momento da captura:

```text
Última atualização 25/10/2026 17:06:27 (Horário local)
Total de seções 499.248
Seções totalizadas 1.225  0,25%
Votação 345.054
Votos válidos 335.743 · Brancos 4.158 · Nulos 5.153
57,66% 193.574 votos  PT – 13  LULA
42,34% 142.169 votos  PL – 22  FLAVIO BOLSONARO
```

Captura visual: [`tse-resultados-2026-10-25.png`](tse-resultados-2026-10-25.png).

## 2. Como a página obtém os dados

O SPA Angular faz requisições XHR/fetch para arquivos JSON no mesmo domínio.
O código carregado pelo SPA (`/oficial/app/*.js`, verificado em 2026-10-25)
monta os caminhos a partir de:

- host `https://resultados.tse.jus.br`;
- ambiente `oficial`;
- config comum `comum/config/ele-c.json`;
- ciclo (ex.: `ele2026`) e código da eleição (ex.: `6258`);
- arquivo unificado por abrangência:
  `dados/<abr>/<abr>-c<cargo4>-e<eleicao6>-u.json`.

Requisições observadas no carregamento e na atualização automática:

```text
GET /oficial/comum/config/ele-c.jws?nocache=<ms>
GET /oficial/ele2026/6258/config/mun-e006258-cm.jws?nocache=<ms>
GET /oficial/ele2026/6258/dados/br/br-c0001-e006258-u.jws?nocache=<ms>
```

Todas responderam `200 application/json`. O sufixo `.jws` contém o mesmo
payload JSON em um JWS compacto (cabeçalho `alg=EdDSA`, `typ=JOSE`) e é o
formato usado pelo SPA com verificação de assinatura no cliente.

Os arquivos **sem assinatura** também estão publicados e foram reproduzidos
com `curl`:

```text
GET /oficial/comum/config/ele-c.json
GET /oficial/ele2026/6258/dados/br/br-c0001-e006258-u.json
```

O coletor usa os `.json` porque não há requisito de validar a assinatura JWS no
servidor. Isso deve ser revisto se o TSE passar a publicar apenas `.jws`.

O payload do `.jws` foi decodificado (base64url) e comparado ao `.json`:
`ele=6258`, `t=2`, `idg=3948414`, `dg=25/10/2026`, `hg=17:06:31`,
`dt=25/10/2026`, `ht=17:06:27`, e os mesmos votos/percentuais.

## 3. Endpoints reais identificados

Base: `https://resultados.tse.jus.br/oficial`

### 3.1 Configuração comum

```text
GET /comum/config/ele-c.json
```

Contém `pl[]` (pleitos), `e[]` (eleições), `cd`, `cdt2`, `nm`, `t` (turno),
`tp` (tipo), `abr[]` e `cp[]` (cargos). Também contém `arq[]` com os padrões de
diretório. Exemplo real (2026):

```text
pleito 3220, ciclo ele2026
  eleição 6257 — Eleição Ordinária Federal - 2026 1º Turno (t=1)
  eleição 6258 — Eleição Ordinária Federal - 2026 2º Turno (t=2)
  eleição 6259 — Eleição Ordinária Estadual - 2026 1º Turno (t=1)
  eleição 6260 — Eleição Ordinária Estadual - 2026 2º Turno (t=2)
```

### 3.2 Resultado unificado por abrangência (usado pelo coletor)

```text
GET /<ciclo>/<eleicao>/dados/<abr>/<arquivo>-c<cargo4>-e<eleicao6>-u.json
```

O prefixo do arquivo é a abrangência:

| Abrangência | Caminho | Exemplo validado |
| --- | --- | --- |
| Brasil | `dados/br/br-...` | `/ele2026/6258/dados/br/br-c0001-e006258-u.json` (200; 3.188 bytes) |
| UF | `dados/<uf>/<uf>-...` | `/ele2026/6257/dados/sp/sp-c0001-e006257-u.json` (200) |
| UF (estadual) | `dados/<uf>/<uf>-...` | `/ele2026/6260/dados/am/am-c0003-e006260-u.json` (200) |
| Município | `dados/<uf>/<uf><municipio5>-...` | `/ele2026/6284/dados/sp/sp67458-c0011-e006284-u.json` (200) |

Observação: para eleições estaduais o config pode listar abrangência `br`, mas
o arquivo por UF também é publicado (validado com 6259/SP e 6260/AM). O coletor
aceita cargo presente na UF exata **ou** no escopo nacional; a ausência do
arquivo é tratada como erro de fonte (404), não como zero votos.

### 3.3 Resultado agregado por UF (não usado no MVP)

```text
GET /<ciclo>/<eleicao>/dados/<uf>/<uf>-e<eleicao6>-ab.json
```

Validado com `/ele2026/6257/dados/br/br-e006257-ab.json` (200; 29.410 bytes).
Contém `abr[]` com totais por UF, seções, eleitorado e votos. Pode ser usado no
futuro para visão nacional por UF.

### 3.4 Configuração de municípios

```text
GET /<ciclo>/<eleicao>/config/mun-e<eleicao6>-cm.json
```

Validado com `/ele2026/6284/config/mun-e006284-cm.json`; contém `abr[].mu[]`
com `cd`, `nm`, `cdi`, `z[]`. Usado para validar o código do município e obter
o nome. O MVP constrói a URL do município a partir do código informado; a
validação de pertencimento ao município/UF é uma evolução recomendada.

## 4. Cabeçalhos, cache e limites observados

Resposta de `br-c0001-e006258-u.json` em 2026-10-25 (via `curl -I`):

```text
HTTP/2 200
last-modified: Sun, 25 Oct 2026 20:06:48 GMT
etag: "b735a2c16565b023e38b6bb444b604f5"
x-ratelimit-limit: 5000, 5000;w=1
x-ratelimit-remaining: 4929
x-ratelimit-reset: 1
cache-control: max-age=57
content-type: application/json
strict-transport-security: max-age=63072000 ; includeSubDomains ; preload
cdn-cache-status: Hit from child
```

- **ETag**: presente. `If-None-Match` retornou `HTTP 304` com corpo vazio.
- **Last-Modified**: presente e também pode ser usado em `If-Modified-Since`.
- **Cache-Control**: `max-age` variou entre ~41s e ~57s nas amostras.
- **Rate limit**: os cabeçalhos `x-ratelimit-limit: 5000, 5000;w=1` sugerem
  5.000 requisições por janela de 1s, mas isso **não** foi testado. O projeto
  não se aproxima disso: o padrão é 1 requisição a cada 120s.
- **CDN**: Akamai (`akamai-grn`, `cdn-cache-status`); respostas podem vir de
  borda com pequena defasagem.
- **Compressão**: o navegador negocia `br/gzip`; `curl` sem `Accept-Encoding`
  recebeu o JSON sem codificação. O transporte Go negocia gzip automaticamente.
- **404**: corpo XML do object storage (`NoSuchKey`), não JSON.

## 5. Frequência de atualização observada

Há duas coisas diferentes: cadência de atualização da página e cadência de
revalidação dos arquivos.

1. O SPA oficial define `TEMPO_PARA_NOVA_REQUISICAO = 12e4` (120.000 ms) no
   código do serviço de seleção. Observamos duas requisições do arquivo de
   resultados no mesmo separador de URL com timestamps `...666028` e
   `...786028` ms: exatamente **120 segundos** de diferença.
2. O cache interno do SPA revalida dados **parciais** a cada **30s** e expira
   em 30 minutos; para `tf=s` (totalização final) guarda por 6 meses. Isso é
   política do cliente, não promessa do servidor.
3. O próprio arquivo informa `dg/hg` (geração) e `dt/ht` (última totalização).
   A última totalização pode permanecer igual por minutos quando não há novas
   seções; ausência de mudança **não** é falha.

Decisão do projeto: polling padrão de **120s**, mínimo de **30s**, jitter e
backoff. O coletor não afirma “tempo real”; afirma “último snapshot oficial”.

## 6. Esquema dos campos usados

Arquivo `*-u.json` (nomes reais da fonte):

| Campo | Significado observado | Uso interno |
| --- | --- | --- |
| `ele` | código da eleição | validação/metadados |
| `t` | turno | metadados |
| `dg`, `hg` | data/hora de geração do arquivo | `GeneratedAt` |
| `dt`, `ht` | data/hora da última totalização | `TotalizedAt` (fallback `dg/hg`) |
| `idg` | identificador de geração | `SourceVersion` e deduplicação |
| `dv` | divulgação de votação (`s`/`n`) | `VotesPublished` |
| `tf` | totalização final (`s`/`n`) | `IsFinal` (única indicação oficial de conclusão) |
| `s.ts`, `s.st` | total/contabilizadas de seções | progresso |
| `s.pst`, `s.pstn` | percentual de seções (texto/exato) | progresso exato |
| `v.tv` | votação total (comparecimento) | total |
| `v.vv` | votos válidos | total |
| `v.vnom` | votos nominais | total |
| `v.vb` | votos brancos | total |
| `v.vn` | votos nulos | total |
| `e.te` | eleitorado apto | total |
| `e.c` | comparecimento | total |
| `e.a` | abstenção | total |
| `carg[].cd/nm*` | cargo | identificação |
| `carg[].agr[].par[]` | partido/federação | partido do candidato |
| `cand[].n` | número eleitoral | chave do candidato |
| `cand[].nm`, `nmu` | nome completo / nome de urna | exibição |
| `cand[].sgp` | sigla do partido (quando presente) | partido |
| `cand[].vap` | votos do candidato | votos |
| `cand[].pvap` | percentual formatado (ex.: `57,66`) | exibição |
| `cand[].pvapn` | percentual exato (ex.: `57.655409048`) | comparação inteira |
| `cand[].st` | situação publicada | exibição |
| `cand[].e` | indicador de eleito (`s`/`n`) | eleito conservador |
| `cand[].dvt` | destino do voto | não usado no MVP |

Percentuais nunca são comparados em ponto flutuante: `pvapn` é convertido para
nano-percentual inteiro (`57.655409048` → `57.655.409.048`). A formatação de
exibição arredonda para 2 casas, como a UI oficial.

## 7. Estado da apuração e conclusão

- `tf=n` → “Em apuração (totalização não final)”.
- `tf=s` → “Totalização final informada pela fonte”.
- `st` de candidato `"2º turno"` com `e=s` no 1º turno significa **classificado
  para o 2º turno**, não eleito. O normalizador marca `Elected=true` apenas
  quando a situação não indica turno e o campo `e` é `s`.
- Nunca inferimos conclusão apenas por `pst=100`; usamos `tf`, que é a indicação
  publicada pela fonte.

## 8. Evidências reproduzíveis

```sh
# 1. Config comum
curl -sS -D - -o /tmp/ele-c.json \
  https://resultados.tse.jus.br/oficial/comum/config/ele-c.json | head

# 2. Resultado ao vivo (2º turno presidencial 2026)
curl -sS -D /tmp/h.txt -o /tmp/u6258.json \
  https://resultados.tse.jus.br/oficial/ele2026/6258/dados/br/br-c0001-e006258-u.json
jq '{ele,t,dg,hg,idg,dt,ht,dv,tf,s,v,cands:[.carg[0].agr[].par[].cand[]|{n,nm,nmu,st,e,vap,pvap,pvapn}]}' /tmp/u6258.json

# 3. Revalidação condicional (deve retornar 304)
ET=$(awk -F': ' 'tolower($1)=="etag"{print $2}' /tmp/h.txt | tr -d '\r')
curl -sS -o /dev/null -w '%{http_code} %{size_download}\n' \
  -H "If-None-Match: $ET" \
  https://resultados.tse.jus.br/oficial/ele2026/6258/dados/br/br-c0001-e006258-u.json

# 4. Variante assinada usada pelo SPA
curl -sS -o /tmp/u6258.jws \
  'https://resultados.tse.jus.br/oficial/ele2026/6258/dados/br/br-c0001-e006258-u.jws?nocache=1'
python3 - <<'PY'
import base64, json
h, p, sig = open('/tmp/u6258.jws').read().strip().split('.')
d = lambda x: base64.urlsafe_b64decode(x + '=' * (-len(x) % 4))
print(d(h).decode())
obj = json.loads(d(p))
print({k: obj[k] for k in ('ele','t','dg','hg','idg','dt','ht','dv','tf')})
print([(c['n'], c['nm'], c['vap'], c['pvapn']) for a in obj['carg'][0]['agr'] for p in a['par'] for c in p['cand']])
PY

# 5. Outros escopos validados
curl -sS -o /dev/null -w '%{http_code}\n' https://resultados.tse.jus.br/oficial/ele2026/6257/dados/sp/sp-c0001-e006257-u.json
curl -sS -o /dev/null -w '%{http_code}\n' https://resultados.tse.jus.br/oficial/ele2026/6260/dados/am/am-c0003-e006260-u.json
curl -sS -o /dev/null -w '%{http_code}\n' https://resultados.tse.jus.br/oficial/ele2026/6284/dados/sp/sp67458-c0011-e006284-u.json
```

Fixtures capturadas em `tests/fixtures/tse/` com hashes e origem em
`tests/fixtures/tse/README.md`.

Referência secundária consultada: `https://github.com/glesiosantos/monitor-tse`
(Go, MIT, outubro/2026). Ela confirma o padrão `-u.json` e o uso de ETag, mas
não foi usada como fonte de verdade; os valores e URLs deste documento foram
reproduzidos diretamente do TSE.

## 9. Limitações e hipóteses não verificadas

- **Não há contrato público de API.** Os caminhos foram obtidos do código do
  SPA e validados por requisições reais, mas podem mudar sem aviso.
- **Assinatura JWS não é verificada** pelo coletor. O `.jws` existe e usa
  EdDSA; a decisão de consumir `.json` está registrada na ADR 0003.
- **2024**: os caminhos testados para a eleição 619 retornaram 404 em
  2026-10-25. Não concluímos se os dados foram migrados/removidos. O projeto
  foi validado apenas para 2026.
- **Município**: a URL foi validada, mas a associação código↔UF não é checada
  antes da coleta; um código inválido gera 404 explícito.
- **Frequência**: 120s é a cadência do SPA, não uma garantia do TSE. Pode haver
  períodos sem atualização (`dt/ht` constante) sem que isso seja falha.
- **Rate limit**: os cabeçalhos indicam 5.000/s, mas não testamos
  intencionalmente o limite. O coletor é deliberadamente conservador.
- **Termos de uso**: os arquivos são públicos e consumidos pelo site oficial;
  não localizamos documentação específica de API. Operadores devem respeitar
  os termos do TSE e não aumentar a frequência sem necessidade.
- **`.ab.json`** foi inspecionado e validado, mas não é usado pelo MVP.
