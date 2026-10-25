# ADR 0002 — Interface de terminal

- Status: aceita
- Data: 2026-10-25

## Contexto e problema

A operação precisa de um painel rápido, legível, interativo por teclado e mouse,
que não bloqueie a coleta. O mesmo binário precisa rodar headless em servidor e
container.

## Critérios

- teclado e mouse (clique/roda);
- layout responsivo;
- estilos e componentes prontos, sem framework pesado;
- teste de lógica/renderização sem terminal gráfico;
- separação rígida entre coleta e apresentação.

## Alternativas

- **tview/tcell**: maduro, mas mais centrado em widgets e menos no modelo
  Elm/Update do Bubble Tea.
- **termui**: visual, porém menos adequado a formulários/modais e manutenção.
- **CLI apenas**: simples, mas não atende ao painel operacional pedido.
- **UI web**: exigiria servidor/frontend e aumenta a superfície.

## Decisão

Bubble Tea + Lip Gloss, com `tea.WithAltScreen` e
`tea.WithMouseCellMotion`. A TUI lê `observability.Status` a cada 500 ms e
chama `Pause/Resume/Refresh` via interface pequena. O app roda a coleta em
goroutine independente; a TUI pode ser desligada (`--no-tui`/sem TTY).

## Consequências

Positivas:

- modelo de estado previsível e testável;
- suporte nativo a mouse, resize e modais;
- estilos adaptáveis a tema claro/escuro.

Negativas:

- renderização manual de tabela/scroll (não usamos o componente `table`);
- testes não cobrem a experiência visual real, apenas conteúdo/dimensões;
- logs em TUI precisam ir para arquivo para não corromper a tela.

## Riscos

- Terminais sem UTF-8/cores podem degradar a aparência; o conteúdo continua
  legível.
- Eventos de mouse dependem do terminal/host.

## Referências

- https://github.com/charmbracelet/bubbletea
- https://github.com/charmbracelet/lipgloss
