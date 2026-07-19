# Coesão M2↔M5 (2026-07-18)

Passada de alinhamento dos planos expandodos. Ordem de implementação: **M2 → M3 → M4 → M5** (M6 fora do MVP).

## Cadeia de contratos

| De → Para | Contrato pinado |
|-----------|-----------------|
| M2 → M3 | `EnvSpec.EnvVars`/`Networks`, `Env.Network`/`Project`/`Ports`, `containerSpec`+`createArgs`, `naming.Network`/`Volume` |
| M2 → M4 | `maxEnvs()` / `limit_exceeded` no create path (M4 **preserva** ao estender o RunE) |
| M3 → M4/M5 | `netpolicy.StartProxy`/`Addr`/`Stop`/`ProxyEnv`/`LoadAllow`/`DefaultAllow`; M3 = mecanismo, M4/M5 = ciclo de vida |
| M4 → M5 | `OpenRegistry`, `DestroyAll`, `MarkEnded`, hooks `startSessionProxy`/`stopSessionProxy`; `session start\|end` (detached) vs `shell` (in-process) |

## Create path final (após M2+M4+M4.5)

```
List → maxEnvs? → OpenRegistry → NextSeq → Name → ProxyEnv(reg.ProxyAddr) → Create → reg.Add
```

## Achados corrigidos nesta passada

1. **M5** tinha `destroyAllInSession` local (`List`+`Destroy`) — substituído por `session.DestroyAll` + `MarkEnded`.
2. **M5 `sbx shell`** não subia o proxy — sem isso, rede `--internal` (M3) fica sem egresso dentro da jail. Agora: start proxy in-process + `SetProxy` no registry (espelha o supervise de M4).
3. **M3** referia "Suposições cross-milestone" na Aceitação sem a seção — adicionada.
4. **M2↔M4** no `newCreateCmd`: M4 documenta composição explícita preservando `maxEnvs` de M2.5; nota de que `Create` pós-M2 usa `containerSpec`, não a assinatura antiga de M1.
5. **M5 `NewRootCmd`**: registra `env` + `session` (M4) + `shell`.

## Não-bloqueadores (intencionais)

- `EnvRecord` sem lista de volumes: teardown de volumes fica no Destroy compose/single de M2 (prefixo `naming.Volume`).
- `EnvSpec.Name` é aditivo em M4; callers diretos do driver sem `Name` mantêm fallback List+`EnvName`.
- Daemon docker-rootless stop no `session end` → M6.
- Endurecimento nftables → M3.5 opcional / Fase 2 Q1.
