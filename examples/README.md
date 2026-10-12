# Examples

Two small projects to try gen-system on. I wrote them for this purpose; they are not production
code. The screenshots in the main README come from them.

| Folder | What it is |
|---|---|
| `payroll/` | A COBOL batch payroll run. It reads employees through the `EMPREC` copybook, computes pay with an `EVALUATE` on the grade, writes payslips and records each run with `EXEC SQL` |
| `shop/` | A Go checkout service. It reserves stock in a SQL transaction, prices the order and writes a receipt file |

Open them in the Studio:

```bash
understand-server --root examples/payroll --copybooks examples/payroll/copybooks
understand-server --root examples/shop
```

Then open http://localhost:28080/. Or ask an agent about them through MCP:

```bash
claude mcp add payroll -- npx -y gen-understand --root examples/payroll --copybooks examples/payroll/copybooks
```
