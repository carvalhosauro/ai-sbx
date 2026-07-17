# ADR 0001 — Arquitetura da camada de ambientes de teste efêmeros

- **Status:** **Aceito** (Fase 1 concluída). Restam só afinações de Fase 2 (§7).
- **Data:** 2026-07-17
- **Autor:** análise assistida (Claude Code)

### Registro de decisões (Fase 1)

| # | Decisão | Escolha | Status |
|---|---|---|---|
| D1 | Caminho arquitetural | **(b) CLI irmã** | ✅ decidido |
| D2 | Linguagem | **Go** | ✅ decidido |
| D3 | SO alvo inicial | **Linux x86_64** (nativo + dentro do WSL2) | ✅ decidido |
| D4 | macOS | **Passo futuro** — arquitetura não pode impedir | ✅ decidido |
| D5 | Topologia do engine | **Ao lado** (side-by-side); endurecimento futuro via microVM | ✅ decidido (§2.1) |
| D6 | Engine do 1º driver | **Podman rootless default + driver Docker-rootless plugável** | ✅ decidido |

**Nota sobre "1 adapter para os 2 PCs":** WSL2 é kernel Linux real. O binário Linux
x86_64 e o driver de container rootless rodam **nativos** no PC pessoal (Linux) e
**dentro da distro WSL2** no PC de trabalho — mesmo código, mesmo engine. "Driver"
no ADR = **backend de container** (Podman/Docker/sysbox/microVM), **não** o SO; SO
não é eixo de driver. Adapter separado só existiria para macOS (seatbelt + engine em
VM), que fica para D4.

---

## 1. Contexto

Queremos dar autonomia real a agentes de IA (Claude Code e similares) para trabalhar
no repositório: subir containers, chamar APIs e rodar testes de integração — com
garantia **inegociável** de que nada escapa para o host, onde há credenciais de
produção e homologação.

A inspiração é o [`akitaonrails/ai-jail`](https://github.com/akitaonrails/ai-jail),
que isola o **processo** do agente. Falta a camada de **ambientes de teste
efêmeros**: o agente precisa subir N ambientes Docker isolados **dentro** da sandbox,
sem conflito entre si e sem tocar no Docker do host. O agente — não a ferramenta —
julga o sucesso; a ferramenta só expõe primitivas de ambiente
(criar, executar, ler logs, inspecionar, destruir).

### 1.1 O que o ai-jail é e faz (levantamento do código-fonte)

Estudo feito sobre o clone da branch `main` (v1.13.x, ~18.4k linhas Rust). Fatos
relevantes:

| Aspecto | Achado |
|---|---|
| **Natureza** | Wrapper de sandbox de **processo**. Linux: `bwrap` (bubblewrap). macOS: `sandbox-exec`/seatbelt (API legada/depreciada da Apple). |
| **Modelo de privilégio** | Rootless. Usa `CLONE_NEWUSER` via bwrap; sem setuid, sem sudo. |
| **Defesa em profundidade (Linux)** | Namespaces PID/UTS/IPC/mount (net só em lockdown); Landlock LSM (V3 FS + V4 net); seccomp-bpf (~30 syscalls bloqueadas); rlimits (NPROC/NOFILE/CORE); máscaras tmpfs sobre `/sys` sensível. |
| **Dotdirs sensíveis** | `~/.aws`, `~/.ssh`, `~/.gnupg`, `~/.mozilla` etc. **nunca** são montados (deny-list embutida). `.env` do projeto exige `--mask`/`--deny-path` explícito (o diretório do projeto é montado inteiro por padrão). |
| **Stack técnica** | Rust síncrono (sem async/tokio), deps mínimas (`lexopt`, `serde`, `toml`, `nix`, `landlock`, `seccompiler`), sem `clap`, saída ANSI crua (**sem `--json`**), `rustfmt` 80 colunas. |
| **Licença** | **GPL-3.0**. |
| **Filosofia declarada** | Backward-compat obsessiva com `.ai-jail` (nunca remove/renomeia campo). "Thin wrapper" — segurança depende do backend do SO. Upstream muito ativo (v0.1 → v1.13, dezenas de releases). |

### 1.2 O passthrough de Docker do ai-jail e sua limitação central

`src/sandbox/bwrap.rs::discover_docker_paths()` (linha ~1931) faz **exatamente uma
coisa**: se `/var/run/docker.sock` existe no host, faz `Mount::Bind` dele para o
mesmo caminho dentro da jail (no WSL2, também expõe o dir de CLI tools do Docker
Desktop). Auto-habilita quando o socket existe; desliga com `--no-docker`.

**Implicação de segurança (crítica para o nosso caso):**

- O agente dentro da jail fala com o **daemon Docker do host**.
- Todo container criado roda no **kernel do host**, fora da sandbox.
- Acesso ao socket Docker é **equivalente a root no host** (basta
  `docker run -v /:/host ...` para montar o filesystem do host e escapar).
- Está documentado pelo próprio ai-jail como "usabilidade sobre lockdown".

Ou seja: **o comportamento default do ai-jail é precisamente o que o nosso produto
proíbe.** O ai-jail oferece o *desligar* (`--no-docker`), mas **não** oferece o
*substituto seguro* (um daemon de containers próprio da sandbox). Essa lacuna é o
núcleo do produto.

### 1.3 A limitação de rede

- Modo normal: **compartilha a stack de rede do host** (sem `--unshare-net`).
- `--lockdown`: `--unshare-net` (sem rede).
- `--lockdown --allow-tcp-port N`: Landlock V4 libera **portas TCP** específicas
  (connect), kernel ≥ 6.5, apenas em lockdown.
- **Não existe allowlist por domínio/host.** Landlock V4 é por porta, não por nome.
  Liberar `api.anthropic.com` (CDN, IPs rotativos) por IP/porta é frágil.

Conclusão: a **allowlist de egresso por domínio** exigida no produto é um componente
que **não existe no ai-jail** e terá de ser construído de qualquer forma
(proxy de egresso com filtro CONNECT/SNI, ou DNS allowlist + ipset/nftables).

### 1.4 O que o ai-jail **não** cobre (o gap do produto)

- Daemon de containers próprio da sandbox (isolado do host).
- Conceito de "ambiente de teste efêmero" com namespace único.
- Ciclo de vida atrelado à sessão do agente (auto-destruição por fim/timeout).
- N ambientes paralelos sem colisão de nomes/redes/volumes/portas dinâmicas.
- Allowlist de rede por domínio.
- Contrato de CLI orientado a agente (`env create/exec/logs/status/destroy`, `--json`).
- Driver plugável (Podman/Docker rootless → sysbox → microVM no futuro).

**Insight-chave:** confinamento de processo (ai-jail) e orquestração de ambientes
(produto) são preocupações **ortogonais**. Elas *compõem*, mas não são a mesma
ferramenta.

---

## 2. Restrição transversal que domina a decisão: "engine dentro da sandbox"

Independente do caminho, o requisito "ambientes rodam num daemon PRÓPRIO da sandbox,
não o do host" tem uma dificuldade técnica central:

- **Rodar um engine de containers *dentro* do bwrap (Linux) é não-trivial.** Precisa
  de user-namespaces aninhados, `newuidmap`/`newgidmap` (subuid/subgid), delegação de
  cgroups, `/dev` adequado, e syscalls que o seccomp do ai-jail **bloqueia** por
  padrão. Rodar Podman rootless *aninhado* em bwrap é possível, porém frágil e
  sensível à política exata do ai-jail.
- **No macOS o problema quase some** (fora do escopo inicial — D4): Podman/Docker já
  rodam numa **VM Linux** (podman machine / Docker Desktop). Isso é um limite mais
  forte (VM) e evita o aninhamento — mas o seatbelt do ai-jail não controla o que roda
  dentro da VM.

Isso gera dois modelos de topologia possíveis, que atravessam os três caminhos:

- **Aninhado:** o engine roda *dentro* do processo já confinado pelo ai-jail.
  Exige cooperação da política do ai-jail (seccomp/subuid/rede) → puxa para o **fork**.
- **Ao lado (side-by-side):** um wrapper de nível superior sobe (1) um engine
  rootless numa isolação que **o produto controla** (storage root, rede e socket
  próprios, sem socket do host) e (2) o agente sob ai-jail, com o socket *desse*
  engine mapeado para dentro via `--rw-map`. O ai-jail permanece intocado.

Este ADR trata a topologia como **decisão acoplada** ao caminho (ver §4 e Questões).

### 2.1 Recomendação detalhada de topologia (D5) — Linux-only

#### Opção "Ao lado" (side-by-side) — **RECOMENDADA**

Fluxo concreto:

1. Um wrapper de entrada (nossa CLI Go, ex.: `sbx`) provisiona um engine rootless
   **do produto**, com recursos totalmente próprios:
   - **storage root próprio** (ex.: `$XDG_DATA_HOME/sbx/<sessão>/storage`), separado
     de qualquer Docker/Podman do host;
   - **socket próprio** (ex.: `$XDG_RUNTIME_DIR/sbx/<sessão>/api.sock`) —
     **nunca** `/var/run/docker.sock`;
   - **rede(s) próprias** por ambiente, alcançando só o proxy de egresso.
2. O agente roda sob ai-jail com:
   - `--no-docker` (socket do host **nunca** exposto);
   - `--rw-map <socket-do-produto>` — o `sbx env ...` do agente só alcança o engine
     do produto;
   - rede restrita ao proxy de egresso (allowlist por domínio).
3. O processo do engine roda **fora** do bwrap do ai-jail, na isolação que o produto
   controla na conta do usuário → **sem** problema de userns aninhado.

Por que isola o suficiente:
- Containers ficam isolados do host pelo **userns rootless do próprio engine** (Podman
  rootless mapeia cada container em subuid não-privilegiado).
- Daemon Docker do host **nunca** é tocado (não bindamos o socket dele).
- Credenciais do host: o ai-jail já esconde `~/.aws`/`~/.ssh`/etc do **processo do
  agente**; os containers recebem só o que injetarmos (`.env.sandbox`), nunca segredo
  do host.
- O socket que o agente alcança controla **só** o engine do produto (storage/rede
  próprios) → blast radius contido.

Custos / riscos residuais:
- O engine rootless roda na **conta do usuário no host** (não dentro do bwrap). Um
  escape de container (exploit de kernel) cairia no usuário do host — não em root,
  mas ainda "no host". Esse é o gap vs. aninhamento/VM.
- Controle total do agente sobre o engine → risco de exaustão de recurso; mitigar com
  limites por sessão (já é requisito).
- A **costura** (engine do produto + agente sob ai-jail) precisa ser cabeada certo.

#### Opção "Aninhado" (nested) — não recomendada agora

- Engine roda **dentro** do bwrap (agente + engine na mesma árvore confinada).
- **Mais forte no papel:** um escape cai dentro da sandbox já confinada, não no host.
- **Mas:** Podman rootless dentro do bwrap exige userns aninhado, `newuidmap`/
  `newgidmap` com subuid disponível lá dentro, delegação de cgroup — e o **seccomp do
  ai-jail bloqueia ~30 syscalls** que o engine precisa (mount, flags de clone, etc.).
  Seria preciso o ai-jail **relaxar a política** → cooperação/patch → puxa para o
  **fork**, exatamente o acoplamento que a decisão D1 evita. Frágil entre kernels;
  WSL2 adiciona complicações de cgroup/systemd.

#### Escalonamento recomendado

**"Ao lado" agora; microVM como passo de endurecimento futuro** (não aninhar no
bwrap). Isso dá uma história de escalada limpa, mantendo o **mesmo contrato de CLI**
(driver plugável):

```
rootless Podman/Docker  →  microVM (Firecracker/krun)  →  [macOS: engine-em-VM]
   (dev, código semi-confiável)   (código hostil)              (D4, futuro)
```

O ganho marginal do aninhamento (escape cai na sandbox vs. no usuário do host) é real,
mas é melhor endereçado por um **driver microVM** do que brigando com o bwrap — e o
caminho microVM já converge com o modelo macOS (engine-em-VM) de D4.

---

## 3. Os três caminhos

### (a) Fork do ai-jail — subcomando `ai-jail sandbox ...` em Rust

Absorver a orquestração dentro do próprio ai-jail, como novo subcomando.

- **Esforço:** Alto. Base de 18k linhas, síncrona (sem async), deps mínimas por
  filosofia. Orquestração de ambientes (gestão de daemon, parse de compose, estado de
  sessão, alocação de portas, proxy de rede) é um subsistema grande e novo — e
  naturalmente quer `shell out` para podman/docker, o que destoa do estilo do repo.
- **Manutenção vs upstream:** Ruim. Upstream é muito ativo e **refatora o interior**
  (o próprio AUDIT relata "16 commits de refactor" preservando comportamento). Um
  subsistema grande no fork paga *rebase tax* perpétuo sobre código sensível a
  segurança. GPL-3.0 obriga o fork a permanecer GPL-3.0.
- **Portabilidade multi-OS:** Herda Linux+macOS do ai-jail (bom). Mas o problema do
  engine-aninhado é Linux-cêntrico de qualquer modo.
- **Superfície de segurança:** **A pior das três.** Editamos exatamente o código que
  *é* a fronteira de segurança. Blast radius máximo; toda mudança arrisca a garantia
  da sandbox. Em compensação, é o único caminho que naturalmente permite estender a
  política bwrap/seccomp para **hospedar um engine aninhado com segurança** (se a
  topologia "aninhada" for obrigatória).
- **Ergonomia para o agente:** Ótima — um binário, um `--help`, um CLAUDE.md.

### (b) CLI irmã — ferramenta separada que compõe com o ai-jail sem modificá-lo

Uma ferramenta nova focada **só** em orquestração de ambientes; ai-jail vira
dependência caixa-preta.

- **Esforço:** Médio. Escopo cirúrgico (só ambientes). `shell out` para
  podman/docker via API/CLI. Liberdade de linguagem e de ecossistema.
- **Manutenção vs upstream:** **A melhor.** ai-jail é *pinado/rastreado* por release,
  não forkado. Zero *rebase tax*. Preocupações desacopladas (SRP).
- **Portabilidade multi-OS:** Boa. A ferramenta e o ai-jail miram Linux/macOS/WSL2.
  No macOS, o engine numa VM Linux resolve o aninhamento de graça.
- **Superfície de segurança:** **A melhor para o requisito inegociável.** Uma
  ferramenta pequena e **auditável de forma independente** que *é dona* do
  daemon e da política de rede. O risco migra do "interior da fronteira" para a
  **costura** entre as duas ferramentas — que é explícita, versionável e testável.
  A costura pode exigir uma *receita de invocação* do ai-jail (flags) ou, no pior
  caso, um pequeno patch upstream **enviável** (não um fork). Licença: por ser
  processo separado invocando ai-jail, **não** é obra derivada → liberdade de licença.
- **Ergonomia para o agente:** Boa. Duas ferramentas, dois `--help`. Mitigável com
  um bom CLAUDE.md e/ou um wrapper único de entrada.

### (c) Do zero — absorver só os conceitos do ai-jail

Reconstruir confinamento de processo **e** orquestração de ambientes.

- **Esforço:** **O maior.** Reinventa o wrapping de bwrap/seatbelt que o ai-jail já
  resolveu e testou em produção (Flatpak-grade). O próprio `sandbox-alternatives.md`
  do ai-jail já avaliou e **rejeitou** reimplementar bwrap em Rust puro.
- **Manutenção vs upstream:** Você é dono de tudo; nenhum aproveitamento de upstream.
- **Portabilidade multi-OS:** Refazer as primitivas Linux+macOS do zero.
- **Superfície de segurança:** **A maior e menos revisada** — péssimo para um
  requisito "inegociável".
- **Ergonomia para o agente:** Controle total de um CLI unificado (única vantagem
  clara), mas a custo desproporcional.

---

## 4. Matriz comparativa

Notas 1 (pior) a 5 (melhor) para o *nosso* objetivo.

| Critério | (a) Fork | (b) CLI irmã | (c) Do zero |
|---|:---:|:---:|:---:|
| Esforço (5 = menor) | 2 | **4** | 1 |
| Manutenção vs upstream | 1 | **5** | 2 |
| Portabilidade multi-OS | 4 | **4** | 2 |
| Superfície de segurança | 2 | **5** | 1 |
| Ergonomia p/ agente | **5** | 4 | 4 |
| Flexibilidade de licença | 1 (GPL herdada) | **5** | 5 |
| Aptidão p/ engine aninhado | **5** | 3 | 4 |
| **Soma** | 20 | **30** | 19 |

O único critério onde o fork vence de forma decisiva é "engine aninhado" — que só
domina **se** a topologia aninhada for obrigatória (ver Questão 3).

---

## 5. Recomendação

**Caminho (b): CLI irmã, desacoplada, que compõe com o ai-jail.**

Justificativa em uma frase: as duas preocupações são ortogonais, o requisito de
segurança é inegociável, e o upstream é ativo demais para forkar código de fronteira
— logo a menor superfície auditável e a menor dívida de manutenção vencem.

Recomendações de implementação (a validar na Fase 2):

1. **Topologia "ao lado" como padrão** (recomendada em §2.1; falta travar em D5): um
   wrapper de entrada sobe um engine rootless que *o produto controla* (storage/rede/
   socket próprios; **nunca** o socket do host) e roda o agente sob ai-jail com apenas
   o socket *desse* engine mapeado. ai-jail intocado, costura explícita.
   Endurecimento futuro por **driver microVM** — não por aninhamento no bwrap.
2. **Linguagem: Go** (D2, decidido) — ecossistema de containers nativo (clients
   oficiais Docker/Podman, libs de compose, bindings maduros), binário estático único.
3. **Escopo inicial: Linux x86_64** (D3, decidido) — roda nativo no PC pessoal (Linux)
   e dentro da distro WSL2 no PC de trabalho; **um único adapter**. macOS (D4) é passo
   futuro: o mesmo contrato de CLI e o driver plugável (rota microVM/engine-em-VM)
   preservam essa porta aberta sem trabalho agora.
4. **Driver plugável desde o dia 1** (D6): o contrato de CLI (`env create/exec/logs/
   status/destroy`, `--json`) não vaza o backend. **1º driver Podman rootless**
   (daemonless → isolação por sessão trivial via `--root`/`--runroot`/rede próprios;
   menor superfície; rootless nativo). **Driver Docker-rootless** como alternativa
   plugável (paridade nativa de `docker compose`). Artefatos são OCI → Dockerfiles/
   imagens/compose do usuário rodam nos dois. Interface preparada para sysbox/microVM.
   Compose sob Podman via socket API Docker-compat (`podman system service`).
5. **Allowlist de egresso por domínio via proxy de egresso** por sessão (rede interna
   só alcança o proxy; proxy filtra CONNECT/SNI + DNS allowlist). Componente do
   produto, ausente no ai-jail.
6. **Ciclo de vida por sessão** com daemon/supervisor que garante `destroy --all` em
   fim de sessão ou timeout, mesmo se o agente esquecer.

---

## 6. Consequências

**Positivas**
- ai-jail permanece dependência versionada; recebemos fixes de upstream de graça.
- Superfície de segurança pequena e auditável isoladamente.
- Liberdade de linguagem e de licença.
- SRP: confinamento de processo ≠ orquestração de ambientes.

**Negativas / riscos**
- Duas ferramentas para o agente aprender → mitigar com CLAUDE.md + wrapper único.
- A **costura** ai-jail↔produto precisa ser especificada e testada (receita de flags,
  ou patch upstream enviável). É o principal risco residual.
- Se a topologia aninhada virar requisito duro, parte da vantagem de (b) sobre (a)
  diminui (precisaria de cooperação/patch do ai-jail).

**Descartado explicitamente** (fora de escopo do produto, reafirmado): motor de
validação declarativa (`sandbox.toml` com asserts / `verify.sh` obrigatório);
qualquer coisa que exija root permanente no host; UI gráfica.

---

## 7. Questões

### Resolvidas (Fase 1)
- **Caminho** → CLI irmã (D1).
- **Linguagem** → Go (D2).
- **SO alvo inicial** → Linux x86_64, nativo + WSL2, um adapter (D3); macOS futuro (D4).
- **Topologia** → ao lado; endurecimento futuro via microVM, não aninhamento (D5).
- **Engine 1º driver** → Podman rootless default + driver Docker-rootless plugável (D6).

### Afinações de Fase 2 (não bloqueiam início; decidir no plano)

1. **Ponto de enforcement da rede:** allowlist por **proxy de egresso + DNS allowlist**
   (nível de rede do container) basta, ou exige enforcement de kernel
   (nftables/Landlock) além do proxy? (Recomendo começar pelo proxy e endurecer.)
2. **Relação com o upstream:** depender do ai-jail como binário externo (instalado à
   parte) ou *vendorizar*/embutir para instalação única?
3. **Alvo de execução:** só suas máquinas de dev, ou também CI/servidor compartilhado
   (muda premissas de multiusuário e de limites de recurso)?
