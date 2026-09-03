package main

import "html/template"

// page is the whole UI. It is one template with no assets, so the server is a
// single binary with nothing to serve alongside it.
//
// The controls are generated from mapgen.Param, so a generator that declares a
// new parameter gets a working control here without this file changing.
var page = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>marajanda &mdash; hex map generators</title>
<style>
  :root {
    color-scheme: light dark;
    --bg: #fbfaf7; --panel: #fff; --ink: #1b1a17; --muted: #6b6862;
    --line: #ddd8cf; --accent: #3a6ea5;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #14140f; --panel: #1c1c17; --ink: #e9e6df; --muted: #96918a;
      --line: #33322b; --accent: #7aa9d8;
    }
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 2rem 1.25rem; background: var(--bg); color: var(--ink);
    font: 15px/1.5 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
  }
  main { max-width: 46rem; margin: 0 auto; }
  h1 { font-size: 1.35rem; margin: 0 0 .25rem; letter-spacing: -.01em; }
  .sub { color: var(--muted); margin: 0 0 1.5rem; }
  fieldset { border: 1px solid var(--line); border-radius: 10px; background: var(--panel);
             padding: 1rem 1.25rem 1.25rem; margin: 0 0 1.25rem; }
  legend { padding: 0 .4rem; color: var(--muted); font-size: .8rem;
           text-transform: uppercase; letter-spacing: .07em; }
  .desc { color: var(--muted); margin: .25rem 0 1rem; }
  .row { display: grid; grid-template-columns: 13rem 1fr; gap: .75rem 1rem;
         align-items: baseline; padding: .45rem 0; border-top: 1px solid var(--line); }
  .row:first-of-type { border-top: 0; }
  label { font-weight: 500; }
  .help { display: block; color: var(--muted); font-size: .85rem; margin-top: .15rem; }
  input[type=number], select {
    width: 100%; max-width: 22rem; padding: .4rem .5rem; font: inherit;
    color: var(--ink); background: var(--bg);
    border: 1px solid var(--line); border-radius: 6px;
  }
  input[type=checkbox] { width: 1.05rem; height: 1.05rem; accent-color: var(--accent); }
  .seed { display: flex; gap: .5rem; max-width: 22rem; }
  button, .btn {
    font: inherit; padding: .4rem .8rem; border-radius: 6px; cursor: pointer;
    border: 1px solid var(--line); background: var(--panel); color: var(--ink);
  }
  button.primary { background: var(--accent); border-color: var(--accent); color: #fff;
                   font-weight: 600; padding: .55rem 1.1rem; }
  .actions { display: flex; gap: .75rem; align-items: center; }
  .note { color: var(--muted); font-size: .85rem; }
  a { color: var(--accent); }
</style>
</head>
<body>
<main>
  <h1>Hex map generators</h1>
  <p class="sub">Pick a generator, adjust, and render. The image opens in a new tab.</p>

  <form method="get" action="/">
    <fieldset>
      <legend>Generator</legend>
      <select name="gen" onchange="this.form.submit()" aria-label="Generator">
        {{range .Generators}}
        <option value="{{.Name}}"{{if eq .Name $.Selected.Name}} selected{{end}}>{{.Title}}</option>
        {{end}}
      </select>
      <p class="desc">{{.Selected.Description}}</p>
    </fieldset>
  </form>

  <form method="get" action="/image" target="_blank">
    <input type="hidden" name="gen" value="{{.Selected.Name}}">
    <fieldset>
      <legend>Parameters</legend>
      {{$v := .Values}}
      {{range .Selected.Params}}
      <div class="row">
        <label for="f-{{.Name}}">{{.Label}}</label>
        <div>
          {{if eq .Kind "seed"}}
            <div class="seed">
              <input type="number" id="f-{{.Name}}" name="{{.Name}}"
                     value="{{index $v .Name}}" min="0" step="1">
              <button type="button" onclick="newSeed('f-{{.Name}}')">New</button>
            </div>
          {{else if eq .Kind "bool"}}
            <input type="checkbox" id="f-{{.Name}}" name="{{.Name}}" value="true"
                   {{if eq (index $v .Name) "true"}}checked{{end}}>
          {{else if eq .Kind "choice"}}
            <select id="f-{{.Name}}" name="{{.Name}}">
              {{$cur := index $v .Name}}
              {{range .Choices}}
              <option value="{{.}}"{{if eq . $cur}} selected{{end}}>{{.}}</option>
              {{end}}
            </select>
          {{else}}
            <input type="number" id="f-{{.Name}}" name="{{.Name}}"
                   value="{{index $v .Name}}"
                   min="{{.Min}}" max="{{.Max}}"
                   step="{{if .Step}}{{.Step}}{{else if eq .Kind "int"}}1{{else}}any{{end}}">
          {{end}}
          {{if .Help}}<span class="help">{{.Help}}</span>{{end}}
        </div>
      </div>
      {{end}}
    </fieldset>

    <div class="actions">
      <button type="submit" class="primary">Render</button>
      <span class="note">Opens a PNG in a new tab. The URL is shareable.</span>
    </div>
  </form>
</main>

<script>
// The seed comes from the server so all randomness stays in one place.
async function newSeed(id) {
  try {
    const r = await fetch('/seed');
    if (r.ok) document.getElementById(id).value = (await r.text()).trim();
  } catch (e) {
    console.error('seed request failed', e);
  }
}
</script>
</body>
</html>
`))
