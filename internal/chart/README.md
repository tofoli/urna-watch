# Mapa do Brasil

`brazil.geojson` é a malha territorial oficial do IBGE, qualidade **mínima**,
com as 27 unidades da federação.

- Fonte: `https://servicodados.ibge.gov.br/api/v3/malhas/paises/BR?formato=application/vnd.geo+json&qualidade=minima&intrarregiao=UF`
- Captura: 2026-10-25
- Propriedade usada: `properties.codarea` (código IBGE da UF)
- Uso: preenchimento por estado vencedor em `RenderMap`; não é usado na coleta.
- Dados públicos do IBGE. Consulte os termos de uso do IBGE ao redistribuir.

O código `internal/chart/map.go` mantém o mapeamento código IBGE → sigla.
