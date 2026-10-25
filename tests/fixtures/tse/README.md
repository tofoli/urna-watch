# Fixtures TSE (dados públicos)

Arquivos JSON reais capturados diretamente de
`https://resultados.tse.jus.br/oficial/` em **2026-10-25**, usados para testes
offline. Não contêm credenciais. Os nomes são os mesmos publicados pela fonte.
Para atualizar, registre a nova data e os hashes; não edite valores à mão sem
criar um fixture sintético separado e identificado.

| Arquivo | Origem | Captura (UTC) | SHA-256 |
| --- | --- | --- | --- |
| `ele-c.json` | `/oficial/comum/config/ele-c.json` | 2026-10-25T21:24Z | `91d1c4fe50dbe92067b83677d77a4f1856f5deca69a04b013a679ff690791721` |
| `br-c0001-e006258-u.json` | `/oficial/ele2026/6258/dados/br/br-c0001-e006258-u.json` | 2026-10-25T21:25Z | `56540dbdd5b563f73e2f18c3299f043a25938589c29809ecc88e434968ea6870` |
| `br-c0001-e006258-u.jws.txt` | `/oficial/ele2026/6258/dados/br/br-c0001-e006258-u.jws?nocache=1` | 2026-10-25T21:25Z | `714a77d5fc723bb72ec3bb8b263395d069e9019595e5126564070f5b68643caf` |
| `br-c0001-e006257-u.json` | `/oficial/ele2026/6257/dados/br/br-c0001-e006257-u.json` | 2026-10-25T21:24Z | `2866703eb4fc0ad6f17f0e081a00ab1175490b0786325e700cff1d8b40d25bca` |
| `br-e006257-ab.json` | `/oficial/ele2026/6257/dados/br/br-e006257-ab.json` | 2026-10-25T21:24Z | `9de639fcd123077cd10697d707d7c939d9a45e56fb5732fce04cea4ca3262a6e` |
| `am-c0003-e006260-u.json` | `/oficial/ele2026/6260/dados/am/am-c0003-e006260-u.json` | 2026-10-25T21:25Z | `5e0eb4e21f5018f4eae7e55319b68a6765178490bfb46338a3448c55fa1be703` |
| `sp67458-c0011-e006284-u.json` | `/oficial/ele2026/6284/dados/sp/sp67458-c0011-e006284-u.json` | 2026-10-25T21:31Z | `82949aea91f8b139e92fd6b52e203de75e72bae5e50ebaec12b98c8bbc8a64a5` |

Notas:

- `br-c0001-e006258-u.json` (2º turno presidencial) tem `tf=n` e 0,25% das
  seções totalizadas no momento da captura.
- `br-c0001-e006257-u.json` (1º turno) tem `tf=s`; os dois primeiros colocados
  aparecem com situação `"2º turno"` e `e=s`, mas não são “eleitos”.
- `am-c0003-e006260-u.json` é um recorte estadual do 2º turno (governador).
- `sp67458-c0011-e006284-u.json` é municipal suplementar com 0 seções
  totalizadas; `dt`/`ht` estão vazios e o normalizador usa `dg`/`hg`.
- O `.jws.txt` é o JWS compacto original; o payload base64url é JSON idêntico
  em conteúdo ao `.json`, com assinatura EdDSA que o projeto não valida.
- Testes que precisam de mudança (votos, seções) geram variações em memória;
  os arquivos reais não são alterados.
