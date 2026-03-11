package main

const htmlTemplates = `{{ define "page" }}<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>ImSub Telegram Message Gallery</title>
  <script type="text/javascript">
    function ShowTextCopied(content) {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(content);
      }
      return false;
    }
  </script>
  <style>
    body {
      margin: 0;
      font: 12px/18px 'Open Sans',"Lucida Grande","Lucida Sans Unicode",Arial,Helvetica,Verdana,sans-serif;
      background: #dfe6eb;
      color: #000;
    }
    strong {
      font-weight: 700;
    }
    code, kbd, pre, samp {
      font-family: Menlo,Monaco,Consolas,"Courier New",monospace;
    }
    code {
      padding: 2px 4px;
      font-size: 90%;
      color: #c7254e;
      background-color: #f9f2f4;
      border-radius: 4px;
    }
    pre {
      display: block;
      margin: 0;
      line-height: 1.42857143;
      word-break: break-all;
      word-wrap: break-word;
      color: #333;
      background-color: #f5f5f5;
      border-radius: 4px;
      overflow: auto;
      padding: 3px;
      border: 1px solid #eee;
      max-height: none;
      font-size: inherit;
    }
    .clearfix:after {
      content: " ";
      visibility: hidden;
      display: block;
      height: 0;
      clear: both;
    }
    .pull_left {
      float: left;
    }
    .pull_right {
      float: right;
    }
    .page_wrap {
      background-color: #ffffff;
      color: #000000;
      min-height: 100vh;
    }
    .page_wrap a {
      color: #168acd;
      text-decoration: none;
    }
    .page_wrap a:hover {
      text-decoration: underline;
    }
    .details {
      color: #70777b;
    }
    .page_body {
      padding-top: 8px;
      width: min(1720px, 100%);
      margin: 0 auto;
    }
    .gallery_intro {
      padding: 16px 24px 4px;
      color: #4b555a;
      font-size: 12px;
      line-height: 1.5;
    }
    .history {
      padding: 16px 0;
    }
    .section_grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, 504px);
      gap: 12px;
      padding: 0 12px;
      align-items: start;
      justify-content: center;
    }
    .message {
      margin: 0 -10px;
      transition: background-color 2s ease;
    }
    .service {
      padding: 10px 24px;
    }
    .service .body {
      text-align: center;
    }
    .message .userpic .initials {
      font-size: 16px;
    }
    .default {
      padding: 10px;
    }
    .default .from_name {
      color: #3892db;
      font-weight: 700;
      padding-bottom: 5px;
    }
    .default .body {
      margin-left: 60px;
    }
    .default .text {
      word-wrap: break-word;
      line-height: 150%;
      unicode-bidi: plaintext;
      text-align: start;
      white-space: pre-wrap;
    }
    .default .reply_to,
    .default .media_wrap {
      padding-bottom: 5px;
    }
    .userpic {
      display: block;
      border-radius: 50%;
      overflow: hidden;
    }
    .userpic .initials {
      display: block;
      color: #fff;
      text-align: center;
      text-transform: uppercase;
      user-select: none;
    }
    .userpic4 {
      background-color: #4f9cd9;
    }
    .scenario_block {
      width: 504px;
      box-sizing: border-box;
      padding: 0 12px 12px;
    }
    .scenario_meta {
      display: inline-block;
      margin-top: 2px;
      font-family: Menlo,Monaco,Consolas,"Courier New",monospace;
      font-size: 11px;
      color: #5a6a72;
    }
    details.raw_toggle {
      margin-top: 8px;
      border: 1px solid #e3e6e8;
      border-radius: 6px;
      background: #fafbfc;
      overflow: hidden;
    }
    details.raw_toggle summary {
      cursor: pointer;
      padding: 6px 8px;
      color: #4f5d65;
      font-weight: 600;
      user-select: none;
    }
    details.raw_toggle pre {
      margin: 0;
      border: 0;
      border-top: 1px solid #e3e6e8;
      border-radius: 0;
      background: #f5f7f8;
      white-space: pre-wrap;
    }
    .bot_buttons_table {
      border-spacing: 0px 2px;
      width: 100%;
      margin-top: 6px;
    }
    .bot_buttons_table a {
      display: block;
      color: inherit;
      text-decoration: none;
    }
    .bot_buttons_table a:hover {
      text-decoration: none;
    }
    .bot_button_row {
      padding: 0;
    }
    .bot_button {
      border-radius: 8px;
      text-align: center;
      vertical-align: middle;
      background-color: #168acd40;
      padding: 0;
    }
    .bot_button.primary {
      background-color: #168acd55;
    }
    .bot_button.success {
      background-color: #64bf4755;
    }
    .bot_button.danger {
      background-color: #ff555540;
    }
    .bot_button_label {
      font-weight: 700;
      line-height: 1.3;
      word-break: break-word;
      padding: 8px 10px;
    }
    .bot_button_icon {
      display: inline-block;
      width: 10px;
      height: 10px;
      margin-right: 6px;
      border-radius: 3px;
      vertical-align: -1px;
      background: currentColor;
      opacity: 0.45;
    }
    .bot_button_empty {
      color: #70777b;
      font-style: italic;
      margin-top: 4px;
    }
    @media (max-width: 560px) {
      .page_body {
        width: 100%;
      }
      .section_grid {
        grid-template-columns: 1fr;
        padding: 0 4px;
      }
      .scenario_block {
        width: auto;
        padding: 0 4px 12px;
      }
    }
  </style>
</head>
<body>
  <div class="page_wrap">
    <div class="page_body chat_page">
      <div class="gallery_intro">
        Curated render of the Telegram bot&apos;s meaningful user-visible states. Generated at {{ .GeneratedAt }}.
        Language: {{ joinLangs .Languages }}.
      </div>
      <div class="history">
        {{ range .Sections }}{{ template "section" . }}{{ end }}
      </div>
    </div>
  </div>
</body>
</html>{{ end }}

{{ define "section" }}
<div class="message service">
  <div class="body details"><strong>{{ .Name }}</strong></div>
</div>
<div class="section_grid">
  {{ range .Scenarios }}{{ template "scenario_card" . }}{{ end }}
</div>
{{ end }}

{{ define "scenario_card" }}
<div class="scenario_block" id="{{ .ID }}">
  <div class="message service">
    <div class="body details">
      <strong>{{ .Title }}</strong><br>
      <span class="scenario_meta">{{ .ID }}</span>
      {{ if .Notes }}<br>{{ .Notes }}{{ end }}
    </div>
  </div>
  {{ range .Cards }}{{ template "message_card" . }}{{ end }}
</div>
{{ end }}

{{ define "message_card" }}
<div class="message default clearfix">
  <div class="pull_left userpic_wrap">
    <div class="userpic userpic4" style="width: 42px; height: 42px">
      <div class="initials" style="line-height: 42px">I</div>
    </div>
  </div>
  <div class="body">
    <div class="from_name">ImSub</div>
    <div class="text">{{ renderHTML .RawText }}</div>
    {{ if .HasButtons }}{{ template "button_table" .Buttons }}{{ else }}<div class="bot_button_empty">No inline keyboard</div>{{ end }}
    <details class="raw_toggle">
      <summary>Show raw Telegram HTML</summary>
      <pre>{{ .RawText }}</pre>
    </details>
  </div>
</div>
{{ end }}

{{ define "button_table" }}
<div class="media_wrap">
  <table class="bot_buttons_table">
    <tbody>
    {{ range . }}
      <tr>
        {{ range . }}
          <td class="bot_button_row">
            <div class="bot_button {{ .Style }}">
              <a href="" onclick="return ShowTextCopied({{ printf "Data: %s | Type: %s" .Target .Kind | printf "%q" }});">
                <div class="bot_button_label">{{ if .HasIcon }}<span class="bot_button_icon" aria-hidden="true"></span>{{ end }}{{ .Label }}</div>
              </a>
            </div>
          </td>
        {{ end }}
      </tr>
    {{ end }}
    </tbody>
  </table>
</div>
{{ end }}`
