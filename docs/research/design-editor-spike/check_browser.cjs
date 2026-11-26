// Automated UI-driven feasibility evidence, not human usability sign-off.
// Agent patches are authored fixtures; no model/provider integration.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
(async () => {
  const browser = await chromium.launch({headless:true, executablePath:process.env.CHROMIUM_PATH || undefined});
  const page = await browser.newPage({viewport:{width:1440,height:1080}});
  page.setDefaultTimeout(5000);
  const evidence = path.join(__dirname, 'evidence');
  await fs.mkdir(evidence,{recursive:true});
  const state = async () => JSON.parse(await page.locator('#state').textContent());
  const revision = value => page.waitForFunction(r => {
    const text=document.querySelector('#state')?.textContent;
    return text && JSON.parse(text).snapshot.revision === r;
  }, value);
  const capture = async name => {
    await page.screenshot({path:path.join(evidence,name + '.png'),fullPage:true});
    await fs.writeFile(path.join(evidence,name + '.json'),JSON.stringify(await state(),null,2) + '\n');
  };
  try {
    await page.goto(process.env.SPIKE_URL);
    await revision('r0');
    const original = await state();
    await fs.writeFile(path.join(evidence,'before.json'), JSON.stringify(original,null,2) + '\n');
    await page.getByRole('button',{name:'Capture agent base'}).click();
    await page.getByLabel('Pin mode').check();
    await page.frameLocator('iframe').locator('[data-edit="cta"]').click();
    await page.getByLabel('Comment').fill('Use a clearer action label');
    await page.getByRole('button',{name:'Add pin'}).click();
    await page.getByLabel('Pin mode').uncheck();
    const title = page.frameLocator('iframe').locator('[data-edit="title"]');
    await title.fill('Workspace for the launch team');
    await title.press('Tab'); await revision('r1');
    await page.getByLabel('Selected node').selectOption('cta');
    await page.getByLabel('Fill').fill('#245e9b');
    await page.getByLabel('Fill').press('Tab'); await revision('r2');
    const handle = await page.frameLocator('iframe').locator('[data-move="cta"]').boundingBox();
    await page.mouse.move(handle.x + handle.width/2,handle.y + handle.height/2);
    await page.mouse.down();
    await page.mouse.move(handle.x + handle.width/2 + 90,handle.y + handle.height/2 + 35,{steps:8});
    await page.mouse.up(); await revision('r3');
    const edited = await state();
    assert.equal(edited.snapshot.document.nodes.title.text,'Workspace for the launch team');
    assert.equal(edited.snapshot.document.nodes.cta.style.background,'#245e9b');
    assert.equal(edited.snapshot.document.nodes.cta.style.x,130);
    assert.equal(edited.snapshot.document.nodes.cta.style.y,315);
    const denied = await page.frameLocator('iframe').locator('body').evaluate(async () => {
      let dom = false, network = false;
      try { parent.document.querySelector('h1').textContent='BREACHED'; } catch {dom=true;}
      try { await fetch('/state'); } catch {network=true;}
      return {dom,network};
    });
    assert.deepEqual(denied,{dom:true,network:true});
    await capture('human-edits');
    await page.getByRole('button',{name:'Apply authored patch',exact:true}).click();
    await page.getByRole('status').filter({hasText:'Stale revision'}).waitFor();
    assert.equal((await state()).snapshot.revision,'r3');
    await page.getByRole('button',{name:'Rebase nonconflicting'}).click(); await revision('r4');
    const grouped = await state();
    assert.deepEqual(grouped.snapshot.document.nodes.title,edited.snapshot.document.nodes.title);
    assert.equal(grouped.snapshot.document.nodes.cta.parent,'details-group');
    assert.equal(grouped.snapshot.document.nodes.cta.style.x + 16,130);
    assert.equal(grouped.snapshot.document.nodes.cta.style.y + 16,315);
    assert.equal(grouped.snapshot.document.nodes.cta.style.background,'#245e9b');
    assert.equal(grouped.feedback[0].revision,'r0');
    assert.equal(grouped.feedback[0].node,'cta');
    assert.equal(grouped.feedback[0].status,'historical-target-present');
    await capture('structural-rebase');
    await page.getByRole('button',{name:'Undo latest transaction'}).click(); await revision('r5');
    assert.deepEqual((await state()).snapshot.document,edited.snapshot.document);
    await page.getByRole('button',{name:'Redo next transaction'}).click(); await revision('r6');
    assert.deepEqual((await state()).snapshot.document,grouped.snapshot.document);
    await page.getByRole('button',{name:'Save session'}).click();
    await page.getByRole('status').filter({hasText:'Saved version 1'}).waitFor();
    const saved = (await state()).session;
    await page.reload(); await revision('r6');
    assert.deepEqual((await state()).session,saved);
    await page.getByLabel('Patch scenario').selectOption('rename');
    await page.getByRole('button',{name:'Capture agent base'}).click();
    const action = page.frameLocator('iframe').locator('[data-edit="cta"]');
    await action.fill('Open launch workspace'); await action.press('Tab'); await revision('r7');
    await page.getByRole('button',{name:'Rebase nonconflicting'}).click();
    await page.getByRole('status').filter({hasText:'Conflict cta.text'}).waitFor();
    assert.equal((await state()).snapshot.document.nodes.cta.text,'Open launch workspace');
    assert.equal((await state()).snapshot.revision,'r7');
    await capture('conflict');
    await page.getByLabel('Patch scenario').selectOption('remove');
    await page.getByRole('button',{name:'Capture agent base'}).click();
    await page.getByRole('button',{name:'Apply authored patch',exact:true}).click(); await revision('r8');
    assert.equal((await state()).feedback[0].status,'orphaned');
    await capture('orphaned');
    await page.getByRole('button',{name:'Undo latest transaction'}).click(); await revision('r9');
    assert.equal((await state()).feedback[0].status,'historical-target-present');
    assert.equal((await state()).snapshot.document.nodes.cta.text,'Open launch workspace');
    await page.getByRole('button',{name:'Save session'}).click();
    await page.getByRole('status').filter({hasText:'Saved version 2'}).waitFor();
    const final = (await state()).session;
    await page.reload(); await revision('r9');
    assert.deepEqual((await state()).session,final);
    const embedded=await browser.newPage();
    await embedded.goto(process.env.SPIKE_URL+'/embedded.html');
    await embedded.getByRole('status').filter({hasText:'Loaded saved version 2'}).waitFor();
    const embeddedState=JSON.parse(await embedded.locator('#state').textContent());
    assert.deepEqual(embeddedState.session,final);
    await fs.writeFile(path.join(evidence,'trace.json'),JSON.stringify({
      evidence:'Automated human-facing UI controls; authored/simulated agent patches; not human usability sign-off',
      phases:await Promise.all(['before','human-edits','structural-rebase','conflict','orphaned'].map(async name=>{
        const data=JSON.parse(await fs.readFile(path.join(evidence,name+'.json'),'utf8'));
        return {name,snapshot:data.snapshot,feedback:data.feedback};
      })),
      finalSnapshot:(await state()).snapshot,finalFeedback:(await state()).feedback,audit:final.audit,
      undoHistory:final.history.map(entry=>({patch:entry.patch})),
    },null,2)+'\n');
    console.log(JSON.stringify({passed:['actual inline text edit','property input edit','pointer drag','opaque parent-DOM/network denial','stale patch refusal','explicit structural rebase preserves text/style/ID/world position','global undo/redo','save/reload including history and pins','same-property conflict refusal','pin retains original revision','deletion orphans pin; undo restores target','same saved session in embedded harness'], evidence:'automated UI-driven; authored agent fixtures; not human usability sign-off'},null,2));
  } catch(error) {
    await page.screenshot({path:path.join(evidence,'failure.png'),fullPage:true});
    console.error('Failure status:',await page.getByRole('status').textContent());
    console.error('Failure state:',JSON.stringify(await state()));
    throw error;
  } finally { await browser.close(); }
})().catch(error => {console.error(error);process.exitCode=1;});
