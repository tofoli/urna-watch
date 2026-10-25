# ADR 0001 — Linguagem e runtime

- Status: aceita
- Data: 2026-10-25
- Decisores: mantenedores do projeto

## Contexto e problema

O monitor precisa fazer polling HTTP frequente, manter estado durável, enviar
notificações e oferecer uma TUI, com distribuição simples e baixo consumo em
servidores/containers. A escolha precisa permitir um binário único, testes
offline e evolução por agentes.

## Critérios de decisão

- binário único e deploy simples;
- HTTP/contexto/concorrência maduros;
- boa biblioteca de TUI;
- baixo consumo para polling;
- testes e observabilidade;
- manutenção e disponibilidade da toolchain.

## Alternativas consideradas

- **Rust**: excelente runtime, mas custo de desenvolvimento e ecossistema TUI
  menos direto para este escopo.
- **Python**: prototipagem rápida, porém distribuição/runtime e concorrência
  menos simples para um binário de servidor.
- **Node/TypeScript**: bom HTTP, mas distribuição e pegada de runtime maiores.
- **C++**: descartado; custo/benefício e segurança de memória piores para o MVP.

## Decisão

Usar **Go 1.27+**, biblioteca padrão para HTTP/JSON/log, TUI em Bubble Tea e
Lip Gloss. Não haverá implementação paralela em C++.

## Consequências

Positivas:

- binário estático, cross-compile e Docker multi-stage triviais;
- `context`, `net/http`, `encoding/json`, `log/slog` e testes nativos;
- boa relação consumo/simplicidade.

Negativas:

- TUI menos rica que stacks dedicadas a UI;
- dependências Charm (Bubble Tea/Lip Gloss/x-ansi) acoplam parte da apresentação;
- ausência de recursos avançados de banco/ORM (intencional).

## Riscos e limitações

- A versão declarada (`go 1.27.3`) exige toolchain recente; CI usa
  `go-version-file`.
- Atualizações de dependências Charm podem exigir ajustes de API.

## Referências

- https://go.dev/doc/
- https://github.com/charmbracelet/bubbletea
- https://github.com/charmbracelet/lipgloss
