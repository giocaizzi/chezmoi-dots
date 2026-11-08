// Frame terminal captures for README matching Shortty style (gradient, padding, drop shadows)
import { writeFileSync, existsSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const home = os.homedir();
const CHROMIUM_PATH = path.join(home, 'Library/Caches/ms-playwright/chromium_headless_shell-1254/chrome-headless-shell-mac-arm64/chrome-headless-shell');
const OUT_FILE = path.resolve('docs/assets/showcase.png');

async function render() {
  let playwright;
  try {
    playwright = await import(path.join(home, 'Documents/github/shortty/node_modules/playwright/index.mjs'));
  } catch {
    console.log('Playwright not available locally, skipping image re-render.');
    return;
  }

  const { chromium } = playwright;
  const browser = await chromium.launch({
    executablePath: existsSync(CHROMIUM_PATH) ? CHROMIUM_PATH : undefined,
    args: ['--no-sandbox'],
  });

  const page = await browser.newPage({
    viewport: { width: 1400, height: 950 },
    deviceScaleFactor: 2,
  });

  const html = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 100vh;
    background: linear-gradient(135deg, #8ec5fc 0%, #a5b4fc 50%, #d8b4fe 100%);
    padding: 80px 100px;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  }
  .window {
    width: 1080px;
    background: #1e1e2e;
    border-radius: 18px;
    box-shadow: 0 40px 100px rgba(20, 16, 60, 0.45), 0 4px 16px rgba(20, 16, 60, 0.25);
    border: 1px solid rgba(255, 255, 255, 0.16);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .titlebar {
    height: 44px;
    background: #181825;
    display: flex;
    align-items: center;
    padding: 0 18px;
    position: relative;
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }
  .buttons {
    display: flex;
    gap: 8px;
  }
  .dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
  }
  .dot.close { background: #ff5f56; border: 1px solid #e0443e; }
  .dot.min { background: #ffbd2e; border: 1px solid #dea123; }
  .dot.max { background: #27c93f; border: 1px solid #1aab29; }
  .title {
    position: absolute;
    left: 0;
    right: 0;
    text-align: center;
    color: #a6adc8;
    font-size: 13px;
    font-weight: 500;
    letter-spacing: -0.01em;
    pointer-events: none;
  }
  .terminal {
    padding: 24px 28px;
    font-family: "JetBrainsMono Nerd Font", "JetBrains Mono", "SF Mono", Menlo, Consolas, monospace;
    font-size: 13.5px;
    line-height: 1.55;
    color: #cdd6f4;
  }
  .prompt-line {
    margin-bottom: 12px;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .user { color: #cba6f7; font-weight: 600; }
  .dir { color: #89b4fa; }
  .cmd { color: #f9e2af; font-weight: 600; }
  pre {
    font-family: inherit;
    font-size: inherit;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .statusline {
    margin-top: 24px;
    padding-top: 14px;
    border-top: 1px solid #313244;
    display: flex;
    gap: 12px;
    font-size: 12.5px;
  }
  .pill {
    background: #313244;
    padding: 3px 10px;
    border-radius: 6px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .c-blue { color: #89b4fa; }
  .c-mauve { color: #cba6f7; }
  .c-green { color: #a6e3a1; }
  .c-yellow { color: #f9e2af; }
  .c-peach { color: #fab387; }
  .c-red { color: #f38ba8; }
  .c-overlay { color: #6c7086; }
</style>
</head>
<body>
  <div class="window" id="capture">
    <div class="titlebar">
      <div class="buttons">
        <div class="dot close"></div>
        <div class="dot min"></div>
        <div class="dot max"></div>
      </div>
      <div class="title">chezmoi-dots — audit & registry</div>
    </div>
    <div class="terminal">
      <div class="prompt-line">
        <span class="user">user@macbook</span>
        <span class="dir">~</span>
        <span style="color:#6c7086;">$</span>
        <span class="cmd">chezmoi dots && chezmoi-dots audit aliases</span>
      </div>
      <pre><span class="c-blue">chezmoi dots — component edit registry</span>

<span class="c-mauve">agents/config</span>        Universal AI agents harness configuration
<span class="c-mauve">agents/core</span>          Agent instructions, core layer (public, every machine)
<span class="c-mauve">agents/personal</span>      Agent instructions, personal layer (encrypted)
<span class="c-mauve">aliases/public</span>       Alias registry, cross-shell definitions
<span class="c-mauve">ghostty</span>              Ghostty terminal configuration
<span class="c-mauve">git</span>                  Git config (identity per role, sign keys)
<span class="c-mauve">mcp</span>                  MCP servers by layer: user, personal, workspace
<span class="c-mauve">models</span>               Model catalog (synced with Epoch ECI & pricing)
<span class="c-mauve">packages</span>             Package registry: tools per OS, pinned release binaries
<span class="c-mauve">prompt</span>               oh-my-posh prompt theme (Catppuccin Mocha)

<span class="c-blue">chezmoi-dots audit aliases</span>
<span class="c-green">✓</span> <span class="c-overlay">zsh:</span>  all aliases accounted for in registry
<span class="c-green">✓</span> <span class="c-overlay">bash:</span> all aliases accounted for in registry</pre>
      <div class="statusline">
        <div class="pill"><span class="c-green">✓</span> <span class="c-green">registry clean</span></div>
        <div class="pill"><span class="c-blue">󰊢</span> <span class="c-blue">main</span></div>
        <div class="pill"><span class="c-overlay"></span> <span class="c-overlay">~/.local/share/chezmoi</span></div>
      </div>
    </div>
  </div>
</body>
</html>`;

  await page.setContent(html);
  await page.waitForTimeout(300);
  const element = await page.$('body');
  const buffer = await element.screenshot();
  writeFileSync(OUT_FILE, buffer);
  await browser.close();
  console.log(`Rendered: ${OUT_FILE}`);
}

render().catch(console.error);
