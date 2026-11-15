// Isolated Chromium smoke test against an explicitly supplied scratch server.
// Never point this at a running operator instance. The profile and output are caller-owned.
import { spawn } from "node:child_process"
import { mkdir, readFile, writeFile, mkdtemp, rm } from "node:fs/promises"
import { resolve } from "node:path"
import { tmpdir } from "node:os"

const url = process.env.LEGATO_THEME_TEST_URL
const browser = process.env.BROWSER
const tokenFile = process.env.LEGATO_THEME_TEST_TOKEN_FILE
if (!url || !browser || !tokenFile || !["localhost", "127.0.0.1"].includes(new URL(url).hostname) || !new URL(url).port || new URL(url).port === "3080") throw new Error("Set BROWSER, LEGATO_THEME_TEST_URL (isolated loopback port, not 3080), LEGATO_THEME_TEST_TOKEN_FILE")
const output = resolve(process.env.LEGATO_THEME_TEST_OUTPUT ?? "/tmp/legato-theme-verification")
await mkdir(output, { recursive: true })
const profile = await mkdtemp(resolve(tmpdir(), "legato-theme-browser-"))
const token = (await readFile(tokenFile, "utf8")).trim()
const child = spawn(browser, ["--headless", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage", "--remote-debugging-pipe", "--blink-settings=primaryHoverType=2,availableHoverTypes=2,primaryPointerType=4,availablePointerTypes=4", `--user-data-dir=${profile}`], { stdio: ["ignore", "ignore", "pipe", "pipe", "pipe"], env: { ...process.env, XDG_CACHE_HOME: profile } })
let sequence = 0, buffer = "", stderr = "", session
const pending = new Map(), exceptions = [], checks = []
child.stderr.on("data", b => stderr += b)
child.stdio[4].on("data", b => {
  buffer += b
  let i
  while ((i = buffer.indexOf("\0")) >= 0) {
    const text = buffer.slice(0, i); buffer = buffer.slice(i + 1)
    if (!text) continue
    const m = JSON.parse(text)
    if (m.id) { const p = pending.get(m.id); if (p) { pending.delete(m.id); m.error ? p.reject(new Error(JSON.stringify(m.error))) : p.resolve(m.result) } }
    if (m.method === "Runtime.exceptionThrown") exceptions.push(m.params.exceptionDetails)
  }
})
function send(method, params = {}, sid = session) {
  return new Promise((resolve, reject) => {
    const id = ++sequence
    const timer = setTimeout(() => { pending.delete(id); reject(new Error("CDP timeout " + method)) }, 20_000)
    pending.set(id, { resolve: x => { clearTimeout(timer); resolve(x) }, reject: e => { clearTimeout(timer); reject(e) } })
    child.stdio[3].write(JSON.stringify({ id, method, params, ...(sid ? { sessionId: sid } : {}) }) + "\0")
  })
}
async function evaluate(expression) {
  const r = await send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true })
  if (r.exceptionDetails) throw new Error(JSON.stringify(r.exceptionDetails))
  return r.result.value
}
async function wait(expression) {
  for (let i = 0; i < 100; i++) { if (await evaluate(expression)) return; await new Promise(r => setTimeout(r, 100)) }
  throw new Error("Timed out: " + expression)
}
async function check(name, expression) {
  const result = await evaluate(expression)
  if (!result) throw new Error("Failed: " + name)
  checks.push({ name, passed: true })
}
async function navigate(path) { await evaluate("window.themeNavigating=true"); await send("Page.navigate", { url: new URL(path, url).href }); await wait('!window.themeNavigating&&!!document.querySelector("nav")') }
async function viewport(width, height = 1000) { await send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile: width < 600 }) }
async function screenshot(name) { const { data } = await send("Page.captureScreenshot", { format: "png", captureBeyondViewport: false }); await writeFile(resolve(output, name), Buffer.from(data, "base64")) }
const clickRadio = (group, name) => `(()=>{const g=document.querySelector('[role="radiogroup"][aria-label="${group}"]');const b=[...g.querySelectorAll('button')].find(b=>b.textContent.trim()==='${name}');b.click();return true})()`
const expected = { "ocular-light": ["Ocular Light", "rgb(245, 243, 237)", "rgb(37, 43, 51)"], "ocular-dark": ["Ocular Dark", "rgb(32, 37, 43)", "rgb(226, 229, 233)"], dracula: ["Dracula", "rgb(40, 42, 54)", "rgb(248, 248, 242)"] }
try {
  const { targetId } = await send("Target.createTarget", { url: "about:blank" }, null)
  session = (await send("Target.attachToTarget", { targetId, flatten: true }, null)).sessionId
  await send("Runtime.enable"); await send("Page.enable")
  await send("Page.addScriptToEvaluateOnNewDocument", { source: `localStorage.setItem('legato:token:local',${JSON.stringify(token)});` })
  await viewport(1440)
  await navigate("/settings#appearance")
  await wait('!!document.querySelector(\'[role="radiogroup"][aria-label="App theme"]\')')
  const settingsSession = session
  let terminalSession
  if (process.env.LEGATO_THEME_TEST_AGENT) {
    const { targetId } = await send("Target.createTarget", { url: "about:blank" }, null)
    terminalSession = (await send("Target.attachToTarget", { targetId, flatten: true }, null)).sessionId
    session = terminalSession
    await send("Runtime.enable"); await send("Page.enable")
    await send("Page.addScriptToEvaluateOnNewDocument", { source: `localStorage.setItem('legato:token:local',${JSON.stringify(token)});` })
    await send("Page.navigate", { url: new URL(`/agents?task=${process.env.LEGATO_THEME_TEST_AGENT}`, url).href })
    await wait('!!document.querySelector(".xterm-screen")')
    await evaluate('window.themeTerminal = document.querySelector(".xterm-screen")')
    await viewport(1440)
    session = settingsSession
  }
  for (const [id, [name, background, foreground]] of Object.entries(expected)) {
    await evaluate(clickRadio("App theme", name))
    await wait(`document.documentElement.dataset.theme===${JSON.stringify(id)}`)
    if (terminalSession) {
      session = terminalSession
      await wait(`document.documentElement.dataset.theme===${JSON.stringify(id)}`)
      await check(`${id} changes a retained live terminal without remounting`, 'window.themeTerminal===document.querySelector(".xterm-screen")')
      await check(`${id} terminal chrome uses the app surface`, `(()=>{const probe=document.createElement('span');probe.style.backgroundColor=getComputedStyle(document.documentElement).getPropertyValue('--theme-surface');return getComputedStyle(document.querySelector('section[aria-label="Terminal"] > div')).backgroundColor===probe.style.backgroundColor})()`)
      await send("Page.bringToFront")
      await new Promise(r => setTimeout(r, 300))
      await screenshot(`${id}-terminal-desktop.png`)
      await viewport(390, 844)
      await check(`${id} terminal fits 390px`, 'document.documentElement.scrollWidth<=innerWidth')
      await screenshot(`${id}-terminal-390.png`)
      await viewport(1440)
      session = settingsSession
      await send("Page.bringToFront")
    }
    await check(`picker renders ${id} canvas and foreground`, `getComputedStyle(document.body).backgroundColor===${JSON.stringify(background)}&&getComputedStyle(document.body).color===${JSON.stringify(foreground)}`)
    await check(`${id} settings uses distinct panel and raised materials with physical depth`, `(()=>{
      const scope=document.getElementById('appearance');
      const panel=getComputedStyle(scope.querySelector('.bg-lw-surface'));
      const raised=getComputedStyle(scope.querySelector('.bg-lw-raised'));
      return new Set([getComputedStyle(document.body).backgroundColor,panel.backgroundColor,raised.backgroundColor]).size===3&&panel.boxShadow!=='none'&&raised.boxShadow!=='none';
    })()`)
    await evaluate('document.getElementById("appearance").scrollIntoView()')
    await screenshot(`${id}-settings-desktop.png`)
    await evaluate("window.themeNavigating=true"); await send("Page.reload"); await wait(`!window.themeNavigating&&!!document.querySelector('[role="radiogroup"][aria-label="App theme"]')&&document.documentElement.dataset.theme===${JSON.stringify(id)}`)
    await check(`reload persists ${id}`, `localStorage.getItem('legato:app-theme')===${JSON.stringify(id)}`)
    await navigate("/board")
    await screenshot(`${id}-board-desktop.png`)
    for (const path of ["/attention", "/plans", "/review", "/reports", "/workflows", "/learning", "/remote", "/audit"]) {
      await navigate(path)
      await check(`${id} ${path} has no leftover fixed dark page canvas`, `![...document.querySelectorAll('#root *')].some(el=>getComputedStyle(el).backgroundColor==='rgb(10, 10, 15)')`)
    }
    await navigate("/board")
    for (const width of [390, 320]) {
      await viewport(width, 844)
      await check(`${id} board fits ${width}px`, "document.documentElement.scrollWidth<=innerWidth")
      await screenshot(`${id}-board-${width}.png`)
      await navigate("/settings#appearance")
      await wait('!!document.querySelector(\'[role="radiogroup"][aria-label="App theme"]\')')
      await evaluate('document.getElementById("appearance").scrollIntoView()')
      await check(`${id} settings fits ${width}px`, "document.documentElement.scrollWidth<=innerWidth")
      await screenshot(`${id}-settings-${width}.png`)
      await navigate("/board")
    }
    await viewport(1440)
    await navigate("/settings#appearance")
    await wait('!!document.querySelector(\'[role="radiogroup"][aria-label="App theme"]\')')
  }
  await check("all original terminal choices remain", `['Legato','Night','Solarized Dark','Classic'].every(name=>[...document.querySelectorAll('[aria-label="Terminal theme"] button')].some(b=>b.textContent.trim()===name))`)
  await evaluate(clickRadio("Terminal theme", "Night"))
  await evaluate(clickRadio("App theme", "Ocular Light"))
  await check("app change preserves explicit terminal preference", "localStorage.getItem('legato:terminal-theme')==='night'")
  await evaluate(clickRadio("Terminal theme", "Follow app"))
  await check("Follow app is selected", `!![...document.querySelectorAll('[aria-label="Terminal theme"] button')].find(b=>b.textContent.trim()==='Follow app'&&b.getAttribute('aria-checked')==='true')`)
  const previewUrl = process.env.LEGATO_THEME_PREVIEW_URL
  if (previewUrl) {
    const fixture = new URL(previewUrl)
    if (!["localhost", "127.0.0.1"].includes(fixture.hostname) || fixture.port === "3080") throw new Error("Fixture must use an isolated loopback port")
    await send("Page.navigate", { url: previewUrl })
    await wait(`!!document.querySelector('[aria-label="App theme"]')`)
    for (const [id, [name]] of Object.entries(expected)) {
      await evaluate("window.themeNavigating=true"); await send("Page.reload"); await wait(`!window.themeNavigating&&!!document.querySelector('[aria-label="App theme"]')`)
      await evaluate(clickRadio("App theme", name))
      await wait(`document.documentElement.dataset.theme===${JSON.stringify(id)}`)
      for (const width of [1000, 390, 320]) {
        await viewport(width, 1100)
        await check(`${id} real component fixture fits ${width}px`, "document.documentElement.scrollWidth<=innerWidth")
        await screenshot(`${id}-components-${width}.png`)
      }
      await viewport(1000, 1100)
      const contrastCheck = `(() => {
        const rgb = value => value.match(/[\\d.]+/g)?.map(Number) ?? [0,0,0,0];
        const blend = (fg, bg) => { const a = fg[3] ?? 1; return fg.slice(0,3).map((c,i) => c*a + bg[i]*(1-a)); };
        const L = rgb => { const c=rgb.slice(0,3).map(v=>v/255).map(v=>v<=.04045?v/12.92:((v+.055)/1.055)**2.4);return c[0]*.2126+c[1]*.7152+c[2]*.0722; };
        const failures=[]; let pairs=0, minimum=Infinity;
        for(const scope of document.querySelectorAll('[data-contrast]')) for(const el of [scope,...scope.querySelectorAll('*')]) {
          if(![...el.childNodes].some(n=>n.nodeType===3&&n.textContent.trim()) || !el.getBoundingClientRect().width) continue;
          const ancestors=[]; for(let n=el;n;n=n.parentElement)ancestors.unshift(n);
          let bg=[255,255,255]; for(const n of ancestors)bg=blend(rgb(getComputedStyle(n).backgroundColor),bg);
          const fg=blend(rgb(getComputedStyle(el).color),bg);const [a,b]=[L(bg),L(fg)].sort((a,b)=>a-b);const ratio=(b+.05)/(a+.05);pairs++;minimum=Math.min(minimum,ratio);
          if(!Number.isFinite(ratio)||ratio<4.5) failures.push({text:el.textContent.slice(0,55),ratio,fg,bg,classes:el.className});
        }
        return {pairs,minimum,failures};
      })()`
      const initial = await evaluate(contrastCheck)
      await writeFile(resolve(output, `${id}-component-contrast.json`), JSON.stringify(initial, null, 2))
      if (initial.failures.length) throw new Error(`Contrast failures ${id}: `+JSON.stringify(initial.failures))
      checks.push({name:`${id} actual component text contrast (${initial.pairs} pairs)`,passed:true})
      await check(`${id} input keeps the required boundary separate from decorative dividers`, `(()=>{
        const input=document.querySelector('[data-contrast="input"] input');
        const style=getComputedStyle(input), root=getComputedStyle(document.documentElement);
        const probe=document.createElement('span');probe.style.color=root.getPropertyValue('--theme-border');
        return style.borderTopStyle==='solid'&&parseFloat(style.borderTopWidth)>=1&&style.borderTopColor===probe.style.color&&root.getPropertyValue('--color-lw-line')!==root.getPropertyValue('--theme-border');
      })()`)
      await evaluate(`(()=>{const style=document.createElement('style');style.id='contrast-positive-control';style.textContent='[data-contrast="buttons"] button:last-child{transition:none!important;color:var(--theme-text)!important;background:var(--theme-text)!important}';document.head.append(style)})()`)
      const poisoned = await evaluate(contrastCheck)
      if (!poisoned.failures.some(item => item.text === "Cancel")) throw new Error("Contrast guard missed deliberate unreadable text")
      await evaluate("document.getElementById('contrast-positive-control').remove()")
      await new Promise(r => setTimeout(r, 200))
      checks.push({name:`${id} contrast guard catches deliberately unreadable component`,passed:true})
      const hover = await evaluate(`(()=>{const r=document.querySelector('[data-contrast="buttons"] button:last-child').getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2}})()`)
      await send("Input.dispatchMouseEvent", {type:"mouseMoved",...hover})
      await new Promise(r => setTimeout(r, 200))
      const hoverState = await evaluate(`(()=>{const b=document.querySelector('[data-contrast="buttons"] button:last-child');const s=getComputedStyle(b);return {hover:b.matches(':hover'),media:matchMedia('(hover:hover)').matches,background:s.backgroundColor,color:s.color}})()`)
      await writeFile(resolve(output, `${id}-hover-state.json`), JSON.stringify(hoverState, null, 2))
      if (!hoverState.hover || !hoverState.media) throw new Error("Desktop hover was not active")
      const hovered = await evaluate(contrastCheck)
      await writeFile(resolve(output, `${id}-hover-component-contrast.json`), JSON.stringify(hovered, null, 2))
      if (hovered.failures.length) throw new Error(`Hover contrast failures ${id}: `+JSON.stringify(hovered.failures))
      checks.push({name:`${id} hover text contrast (${hovered.pairs} pairs)`,passed:true})
      await send("Input.dispatchMouseEvent", {type:"mouseMoved",x:0,y:0})
      await writeFile(resolve(output, `${id}-fixture-dom.html`), await evaluate("document.documentElement.outerHTML"))
      await evaluate(`document.querySelector('[aria-label="Select new line 2"]').click()`)
      const selected = await evaluate(contrastCheck)
      await writeFile(resolve(output, `${id}-selected-component-contrast.json`), JSON.stringify(selected, null, 2))
      if (selected.failures.length) throw new Error(`Selected contrast failures ${id}: `+JSON.stringify(selected.failures))
      checks.push({name:`${id} selected diff text contrast (${selected.pairs} pairs)`,passed:true})
      await screenshot(`${id}-components-selected.png`)
      await evaluate(`document.querySelector('input[type="text"],input:not([type])').focus()`)
      await send("Input.dispatchKeyEvent", {type:"keyDown",key:"Tab",code:"Tab",windowsVirtualKeyCode:9})
      await send("Input.dispatchKeyEvent", {type:"keyUp",key:"Tab",code:"Tab",windowsVirtualKeyCode:9})
      await check(`${id} keyboard focus is visible`, `document.activeElement.matches(':focus-visible')&&getComputedStyle(document.activeElement).outlineStyle==='solid'&&parseFloat(getComputedStyle(document.activeElement).outlineWidth)>=2`)
      await evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent==='View sample image').click()`)
      await wait(`!!document.querySelector('[role="dialog"]')`)
      await evaluate(`document.querySelector('[role="dialog"]').setAttribute('data-contrast','media')`)
      const media = await evaluate(contrastCheck)
      if (media.failures.length) throw new Error(`Media control contrast failures ${id}: `+JSON.stringify(media.failures))
      checks.push({name:`${id} image preview controls retain readable text`,passed:true})
      await evaluate(`document.querySelector('[aria-label="Close image"]').click()`)
    }
  }
  if (exceptions.length) throw new Error("Browser exceptions: " + JSON.stringify(exceptions))
  checks.push({ name: "no browser JavaScript exceptions", passed: true })
  await writeFile(resolve(output, "verification.json"), JSON.stringify({ browser: (await send("Browser.getVersion", {}, null)).product, checks, limitations: "Isolated Chromium on Linux. Not native WebKit/Android/device execution or a full accessibility audit." }, null, 2) + "\n")
  console.log(`${checks.length} checks passed; ${output}`)
} finally {
  await writeFile(resolve(output, "browser-stderr.log"), stderr)
  await send("Browser.close", {}, null).catch(() => {})
  child.kill()
  await rm(profile, { recursive: true, force: true })
}
