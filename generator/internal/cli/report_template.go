// cli/report_template.go — v23.30 (gen report): the standalone HTML template
// used by WriteReport. Dark-terminal validation-report style per the project
// DELIVERABLE FORMAT spec: IBM Plex Mono, two-column tables, file:line
// evidence. Google Fonts <link> is cosmetic only — the page renders with the
// monospace fallback when offline (HARD CONSTRAINT: no CDN for layout).
package cli

const reportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet">
<style>
  :root {
    --bg:        #0b0e14;
    --panel:     #11151f;
    --panel-2:   #161b27;
    --ink:       #d6dbe5;
    --ink-dim:   #8a93a6;
    --line:      #232a39;
    --accent:    #5ad1a0;
    --accent-dim:#2c6f57;
    --warn:      #e0b341;
    --fail:      #e06c6c;
    --mono: "IBM Plex Mono", ui-monospace, "SF Mono", "Cascadia Mono",
            "Roboto Mono", Menlo, Consolas, monospace;
  }
  * { box-sizing: border-box; }
  html, body { margin: 0; padding: 0; }
  body {
    background: var(--bg);
    color: var(--ink);
    font-family: var(--mono);
    font-size: 13.5px;
    line-height: 1.55;
    -webkit-font-smoothing: antialiased;
  }
  .wrap { max-width: 1040px; margin: 0 auto; padding: 40px 28px 80px; }

  header.masthead {
    border: 1px solid var(--line);
    background: linear-gradient(180deg, var(--panel-2), var(--panel));
    border-radius: 8px;
    padding: 22px 24px;
    margin-bottom: 26px;
  }
  .masthead .tag {
    color: var(--accent);
    font-size: 11px;
    letter-spacing: 0.18em;
    text-transform: uppercase;
  }
  .masthead h1 {
    font-size: 19px;
    font-weight: 600;
    margin: 8px 0 14px;
    line-height: 1.3;
    color: #fff;
  }
  .meta-grid {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 3px 18px;
    font-size: 12px;
  }
  .meta-grid dt { color: var(--ink-dim); }
  .meta-grid dd { margin: 0; color: var(--ink); word-break: break-all; }

  .highlights {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 12px;
    margin: 26px 0;
  }
  .hl {
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--panel);
    padding: 14px 16px;
  }
  .hl .v { font-size: 20px; font-weight: 600; color: var(--accent); }
  .hl .l { font-size: 11px; color: var(--ink-dim); text-transform: uppercase; letter-spacing: 0.08em; margin-top: 4px; }
  .hl .s { font-size: 11px; color: var(--ink-dim); margin-top: 2px; }

  h2.section {
    font-size: 13px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--ink-dim);
    border-bottom: 1px solid var(--line);
    padding-bottom: 8px;
    margin: 40px 0 18px;
  }

  .suite { margin-bottom: 26px; }
  .suite-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 8px;
  }
  .suite-head .name { font-weight: 600; color: #fff; font-size: 14px; }
  .suite-head .name::before { content: "› "; color: var(--accent-dim); }
  .pill {
    font-size: 11px;
    padding: 2px 9px;
    border-radius: 999px;
    border: 1px solid var(--line);
  }
  .pill.ok   { color: var(--accent); border-color: var(--accent-dim); }
  .pill.bad  { color: var(--fail);   border-color: var(--fail); }
  .notes { font-size: 11px; color: var(--ink-dim); margin: 0 0 10px; }

  table { width: 100%; border-collapse: collapse; font-size: 12px; }
  th {
    text-align: left;
    font-weight: 500;
    color: var(--ink-dim);
    border-bottom: 1px solid var(--line);
    padding: 6px 10px;
    font-size: 11px;
    letter-spacing: 0.04em;
  }
  td {
    padding: 6px 10px;
    border-bottom: 1px solid var(--line);
    vertical-align: top;
  }
  tr:last-child td { border-bottom: none; }
  td.k { color: var(--ink); white-space: nowrap; }
  td.v { color: var(--ink); }
  td.status { white-space: nowrap; width: 1%; }
  .ok-mark   { color: var(--accent); }
  .fail-mark { color: var(--fail); }
  .want { color: var(--ink-dim); }
  .metric-table td.v { color: var(--accent); }
  .unit { color: var(--ink-dim); font-size: 11px; }

  .skill-cols {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 22px;
  }
  @media (max-width: 720px) { .skill-cols { grid-template-columns: 1fr; } }

  footer {
    margin-top: 50px;
    border-top: 1px solid var(--line);
    padding-top: 16px;
    font-size: 11px;
    color: var(--ink-dim);
  }
  footer code { color: var(--ink); }
  a { color: var(--accent); }
</style>
</head>
<body>
<div class="wrap">

  <header class="masthead">
    <div class="tag">gen-system · code-intelligence validation</div>
    <h1>{{.Title}}</h1>
    <dl class="meta-grid">
      {{if .ProjectRoot}}<dt>project</dt><dd>{{.ProjectRoot}}</dd>{{end}}
      <dt>generated</dt><dd>{{.GeneratedAt}}</dd>
      {{if .ModelLine}}<dt>model</dt><dd>{{.ModelLine}}</dd>{{end}}
      {{if .HostLine}}<dt>host</dt><dd>{{.HostLine}}</dd>{{end}}
      <dt>result</dt>
      <dd>
        {{if eq .TotalFail 0}}<span class="ok-mark">{{.TotalPass}} pass / 0 fail</span>
        {{else}}<span class="fail-mark">{{.TotalPass}} pass / {{.TotalFail}} fail</span>{{end}}
        across {{.SuiteCount}} suite(s)
      </dd>
    </dl>
  </header>

  {{if .Highlights}}
  <div class="highlights">
    {{range .Highlights}}
    <div class="hl">
      <div class="v">{{.Value}}</div>
      <div class="l">{{.Label}}</div>
      {{if .Sub}}<div class="s">{{.Sub}}</div>{{end}}
    </div>
    {{end}}
  </div>
  {{end}}

  {{range .Runs}}
  <h2 class="section">Suites — {{if .ProjectRoot}}{{.ProjectRoot}}{{else}}run{{end}}</h2>
  {{range .Suites}}
  <div class="suite">
    <div class="suite-head">
      <span class="name">{{.Name}}</span>
      {{if eq .Fail 0}}<span class="pill ok">{{.Pass}}/{{.Pass}} ok</span>
      {{else}}<span class="pill bad">{{.Fail}} fail</span>{{end}}
    </div>
    {{if .Notes}}<p class="notes">{{.Notes}}</p>{{end}}

    {{if .Cases}}
    <table>
      <thead><tr><th>check</th><th>evidence (got)</th><th>expected</th><th class="status"></th></tr></thead>
      <tbody>
      {{range .Cases}}
        <tr>
          <td class="k">{{.Name}}</td>
          <td class="v">{{.Got}}{{if .Note}}<br><span class="want">{{.Note}}</span>{{end}}</td>
          <td class="want">{{.Want}}</td>
          <td class="status">{{if .Pass}}<span class="ok-mark">PASS</span>{{else}}<span class="fail-mark">FAIL</span>{{end}}</td>
        </tr>
      {{end}}
      </tbody>
    </table>
    {{end}}

    {{if .Metrics}}
    <table class="metric-table" style="margin-top:10px;">
      <thead><tr><th>metric</th><th>value</th><th>unit</th></tr></thead>
      <tbody>
      {{range .Metrics}}
        <tr>
          <td class="k">{{.Name}}</td>
          <td class="v">{{.Value}}</td>
          <td class="unit">{{.Unit}}</td>
        </tr>
      {{end}}
      </tbody>
    </table>
    {{end}}
  </div>
  {{end}}
  {{end}}

  {{if .Skill}}
  <h2 class="section">Exported symbol graph — {{.Skill.FileName}}</h2>
  <dl class="meta-grid" style="margin-bottom:16px;">
    {{if .Skill.ProjectRoot}}<dt>root</dt><dd>{{.Skill.ProjectRoot}}</dd>{{end}}
    {{if .Skill.WrittenAt}}<dt>written</dt><dd>{{.Skill.WrittenAt}}</dd>{{end}}
    <dt>nodes</dt><dd>{{.Skill.NodeCount}}</dd>
    <dt>edges</dt><dd>{{.Skill.EdgeCount}}</dd>
  </dl>
  <div class="skill-cols">
    <div>
      <table>
        <thead><tr><th>language</th><th>symbols</th></tr></thead>
        <tbody>
        {{range .Skill.ByLanguage}}<tr><td class="k">{{.Key}}</td><td class="v">{{.Value}}</td></tr>{{end}}
        </tbody>
      </table>
    </div>
    <div>
      <table>
        <thead><tr><th>layer</th><th>symbols</th></tr></thead>
        <tbody>
        {{range .Skill.ByLayer}}<tr><td class="k">{{.Key}}</td><td class="v">{{.Value}}</td></tr>{{end}}
        </tbody>
      </table>
    </div>
  </div>
  {{end}}

  <footer>
    Rendered by <code>gen report</code> from genbench <code>genbench/v1</code> artefacts.
    Parser-path suites are deterministic and reproducible from source via
    <code>freeze-cobol-baseline.sh verify</code>. No metric in this report is
    model-generated; recall figures count resolvable internal call edges.
  </footer>

</div>
</body>
</html>`
