# ADR 0007 — Implantação e CI

- Status: aceita
- Data: 2026-10-25

## Contexto

O monitor deve rodar nativamente e em Docker, sem privilégios, com estado
persistente e configuração por ambiente. O CI precisa validar formatação, lint,
testes, build e vulnerabilidades sem publicar releases automaticamente.

## Critérios

- execução headless confiável;
- container sem root, raiz read-only, volume de dados;
- sinais/encerramento gracioso;
- CI reproduzível;
- segredos nunca na imagem;
- versionamento manual até haver estratégia.

## Decisão

- **Makefile** para build/test/lint/check e Docker.
- **Dockerfile** multi-stage (`golang:1.27-alpine` → `alpine:3.22`), binário
  estático `CGO_ENABLED=0`, usuário `urna`, `ca-certificates`/`tzdata`.
- **Compose** com `restart`, healthcheck `/healthz`, `read_only`, tmpfs,
  `no-new-privileges`, volume nomeado `urna-data` (ou bind mount com posse 100:101), porta de status só em localhost.
- **GitHub Actions** com `setup-go` (versão do `go.mod`), `gofmt -l`,
  `go vet`, `go test -race`, `go build` e `govulncheck`.
- Sem release/publicação automática; `CHANGELOG` e tags são manuais.

## Alternativas

- Kubernetes/Helm: complexidade desnecessária para uma instância.
- Binário em VM/systemd: suportado, documentado como exemplo.
- Scratch/distroless: exigiria copiar CA/tzdata e complica debug; Alpine é
  suficiente e pequeno.
- Publicação automática: adiada (sem política de versionamento definida).

## Consequências

Positivas:

- deploy simples e reproduzível;
- superfície reduzida;
- healthcheck útil para orquestração;
- CI detecta regressões e dependências vulneráveis.

Negativas:

- imagem Alpine adiciona ~8–15 MB;
- `read_only` exige volume para estado/log;
- rodar TUI em container não é suportado (headless).

## Riscos

- Tags de imagem `1.27`/`3.22` precisam existir/ser mantidas; fixe por digest
  em produção se necessário.
- `govulncheck` pode falhar por vulnerabilidade transitiva; avalie e atualize.
- Não há assinatura de imagem/release no MVP.

## Referências

- https://docs.docker.com/
- https://docs.github.com/actions
- `Dockerfile`, `compose.yaml`, `.github/workflows/ci.yml`
