// Runs against a disposable mock VPN20 with a genuine standalone relay ZIP.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawn, execFileSync } = require("node:child_process");
const { once } = require("node:events");
const { chromium } = require("playwright");

async function main() {
  const binary = process.env.VPN20_UI_BINARY;
  assert(binary, "Set VPN20_UI_BINARY to the native VPN20 executable");
  const work = fs.mkdtempSync(path.join(os.tmpdir(), "vpn20-relay-ui-"));
  const root = path.resolve(__dirname, "../..");
  let server;
  let browser;
  try {
    execFileSync("python3", ["-c", `
import importlib.util,pathlib,sys
root=pathlib.Path(sys.argv[1]);work=pathlib.Path(sys.argv[2])
spec=importlib.util.spec_from_file_location("fixtures",root/"deploy/relay/tests/test_relay.py")
fixtures=importlib.util.module_from_spec(spec);spec.loader.exec_module(fixtures);fixtures.certificate(work)
spec=importlib.util.spec_from_file_location("relay",root/"deploy/relay/relay.py")
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
manager=m.Manager(work/"vps");manager.init("relay.example.test",443,20001,20100,18921,"1MB")
(manager.root/"ca.pem").write_bytes((work/"ca.pem").read_bytes());manager.add("office")
`, root, work], { stdio: "pipe" });
    server = spawn(binary, [], { stdio: "ignore", env: {
      ...process.env,
      VPN20_DATA_DIR: path.join(work,"app"),
      VPN20_HTTP_LISTEN: "127.0.0.1:52621",
      VPN20_BACKEND: "mock", VPN20_ENDPOINT: "direct.example.test",
      VPN20_MANAGE_SYSCTL: "false", VPN20_MANAGE_FIREWALL: "false", VPN20_UPDATE_MODE: "disabled",
    }});
    browser = await chromium.launch({ headless: true });
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 } });
    const url = "http://127.0.0.1:52621";
    let ready = false;
    for (let i=0; i<100; i++) {
      try {
        const response = await context.request.get(url+"/api/health", { timeout: 1000 });
        if (response.ok()) { ready=true; break; }
      } catch {}
      await new Promise(resolve=>setTimeout(resolve,100));
    }
    assert(ready,"Disposable VPN20 did not start");
    const setup = await context.request.post(url+"/api/setup",{data:{
      username:"admin",password:"local-ui-fixture-password",endpointHost:"direct.example.test",
    }});
    assert.equal(setup.status(),201,"Setup failed");
    const page = await context.newPage();
    const jsErrors=[];
    page.on("pageerror", error=>jsErrors.push(error.message));
    await page.goto(url+"/settings");
    const card = page.locator("form.card").filter({has:page.getByRole("heading",{name:"Server relay",exact:true})});
    const toggle = card.getByRole("checkbox");
    await toggle.check();
    await card.getByLabel("Domain relay",{exact:true}).fill("relay.example.test");
    const save=card.getByRole("button",{name:"Cập nhật relay",exact:true});
    async function apply(expected) {
      const result=page.waitForResponse(r=>r.url()===url+"/api/relay" && r.request().method()==="PUT");
      await save.click();
      assert.equal((await result).status(),expected,"Relay save response");
    }
    await apply(400); // Domain alone cannot bypass profile credentials.
    await card.getByText("Nhập ZIP profile do VPS cấp trước khi bật relay.",{exact:true}).waitFor();
    await card.getByLabel("ZIP profile relay",{exact:true}).setInputFiles(path.join(work,"vps/clients/office/vpn20-relay-office.zip"));
    await apply(202);
    await card.getByText("relay.example.test:20001",{exact:true}).waitFor();
    const status=await (await context.request.get(url+"/api/relay")).json();
    assert.equal(status.enabled,true);
    assert.equal(status.domain,"relay.example.test");
    assert.equal(status.remotePort,20001);
    const overview=await (await context.request.get(url+"/api/status")).json();
    assert.equal(overview.clientEndpoint,"relay.example.test:20001","Dashboard endpoint mismatch");
    for(const field of ["accessKey","token","ca","profileZip"]) assert(!(field in status),"Private data in status");
    const peerResponse=await context.request.post(url+"/api/peers",{data:{
      name:"UI relay device",clientRoutes:"10.8.0.0/24",dns:"",keepalive:null,mtu:null,expiresAt:null,
    }});
    assert.equal(peerResponse.status(),201);
    const peer=await peerResponse.json();
    const config=await (await context.request.get(url+"/api/peers/"+peer.id+"/config")).text();
    assert(config.includes("Endpoint = relay.example.test:20001"),"Downloaded config used a direct endpoint");
    await page.reload();
    await card.getByText("relay.example.test:20001",{exact:true}).waitFor();
    assert.equal(await toggle.isChecked(),true,"Reload lost enabled state");
    await page.setViewportSize({width:390,height:844});
    await card.scrollIntoViewIfNeeded();
    const box=await card.boundingBox();
    assert(box && box.x>=0 && box.x+box.width<=391,"Relay card overflows mobile viewport");
    if(process.env.VPN20_UI_SCREENSHOT) await card.screenshot({path:process.env.VPN20_UI_SCREENSHOT});
    await toggle.uncheck();
    await apply(202);
    const direct=await (await context.request.get(url+"/api/peers/"+peer.id+"/config")).text();
    assert(direct.includes("Endpoint = direct.example.test:51820"),"Disable did not restore direct endpoint");
    assert.deepEqual(jsErrors,[],"Browser runtime errors");
    console.log("Browser relay enable/import/reload/export/disable and mobile layout PASS");
  } finally {
    if(browser) await browser.close();
    if(server && server.exitCode===null) {
      const exited=once(server,"exit");
      server.kill("SIGTERM");
      await exited;
    }
    fs.rmSync(work,{recursive:true,force:true});
  }
}
main().catch(error=>{ console.error(error); process.exitCode=1; });
