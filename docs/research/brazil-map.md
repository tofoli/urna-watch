# Mapa do Brasil — fonte da malha

O mapa do Telegram é uma visualização derivada dos totais oficiais por UF; ele
**não** é uma projeção eleitoral nem substitui a página do TSE.

- Malha: IBGE — API de malhas territoriais
  `https://servicodados.ibge.gov.br/api/v3/malhas/paises/BR?formato=application/vnd.geo+json&qualidade=minima&intrarregiao=UF`
- Captura: 2026-10-25
- Arquivo: `internal/chart/brazil.geojson` (27 UFs, ~98 KB)
- Propriedade usada: `properties.codarea` (código IBGE da UF)
- Projeção: equirretangular simples, ajustada ao painel; apenas visual.
- A mesma malha é rasterizada para a TUI (blocos `▀` com cores) e para o PNG
  enviado ao Telegram; a TUI usa uma grade menor, então estados muito pequenos
  podem ficar com poucos pixels.
- Cores: atribuídas por posição do candidato no snapshot Brasil; estados sem
  votos publicados ficam cinza.
- Vencedor por UF: candidato com mais votos no arquivo oficial `-u.json` da UF.
  Em caso de empate, a ordem normalizada (posição/número) decide a cor.
- Dados do IBGE são públicos; consulte os termos de uso do IBGE para
  redistribuição. O mapa não é afiliado ao TSE.

Limitações:

- As 27 UFs podem ter gerações diferentes; o mapa pode ficar atrás do
  consolidado Brasil.
- O mapa mostra pluralidade de votos por UF, não “eleito” nem projeção.
- A ausência de uma UF é exibida em cinza, nunca como zero.
