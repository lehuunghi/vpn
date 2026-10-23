// Password enrollment against the real VPS TLS frontend and native frps.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const dgram = require("node:dgram");
const { randomBytes } = require("node:crypto");
const { spawn } = require("node:child_process");
const { once } = require("node:events");
const { chromium } = require("playwright");
const pause = ms => new Promise(resolve => setTimeout(resolve, ms));

async function main() {
  assert(process.env.VPN20_UI_BINARY && process.env.VPN20_FRPS_BINARY, "Set both native fixture binaries");
  const work = fs.mkdtempSync(path.join(os.tmpdir(), "vpn20-relay-ui-"));
  const root = path.resolve(__dirname, "../..");
  let server, vps, browser;
  async function stop(child) {
    if (child && child.exitCode === null) { const exited=once(child,"exit"); child.kill("SIGTERM"); await exited; }
  }
  try {
    vps=spawn("python3",[path.join(root,"deploy/relay/tests/ui_fixture.py"),work,process.env.VPN20_FRPS_BINARY],{stdio:"ignore"});
    for(let i=0;i<200 && !fs.existsSync(path.join(work,"ready.json"));i++) await pause(100);
    assert(fs.existsSync(path.join(work,"ready.json")),"VPS fixture failed to start");
    const fixture=JSON.parse(fs.readFileSync(path.join(work,"ready.json"),"utf8"));
    const start=()=>spawn(process.env.VPN20_UI_BINARY,[],{stdio:"ignore",env:{...process.env,
      SSL_CERT_FILE:path.join(work,"ca.pem"),VPN20_DATA_DIR:path.join(work,"app"),
      VPN20_HTTP_LISTEN:"127.0.0.1:52621",VPN20_PORT:String(fixture.local),
      VPN20_BACKEND:"mock",VPN20_ENDPOINT:"direct.example.test",
      VPN20_MANAGE_SYSCTL:"false",VPN20_MANAGE_FIREWALL:"false",VPN20_UPDATE_MODE:"disabled",
    }});
    server=start();
    browser=await chromium.launch({headless:true});
    const context=await browser.newContext({viewport:{width:1280,height:900}});
    const url="http://127.0.0.1:52621";
    async function ready() {
      for(let i=0;i<150;i++) {
        try { if((await context.request.get(url+"/api/health",{timeout:1000})).ok()) return; } catch {}
        await pause(100);
      }
      throw Error("Disposable VPN20 did not start");
    }
    async function connected() {
      for(let i=0;i<200;i++) {
        const s=await (await context.request.get(url+"/api/relay")).json();
        if(s.phase==="connected") return s;
        await pause(100);
      }
      throw Error("Embedded client did not connect to real VPS");
    }
    async function exchange(port) {
      const sock=dgram.createSocket("udp4");
      try {
        for(const size of [32,148,1452]) {
          const payload=randomBytes(size);
          const message=Promise.race([once(sock,"message"),pause(5000).then(()=>{throw Error("Relay UDP timeout");})]);
          sock.send(payload,port,"127.0.0.1");
          const [reply]=await message;
          assert.deepEqual(reply,payload,"Real embedded relay changed UDP payload");
        }
      } finally { sock.close(); }
    }
    await ready();
    assert.equal((await context.request.post(url+"/api/setup",{data:{username:"admin",password:"local-ui-fixture-password",endpointHost:"direct.example.test"}})).status(),201);
    const page=await context.newPage();
    const jsErrors=[];page.on("pageerror",error=>jsErrors.push(error.message));
    await page.goto(url+"/settings");
    const card=page.locator("form.card").filter({has:page.getByRole("heading",{name:"Server relay",exact:true})});
    assert.equal(await card.locator('input[type="file"]').count(),0,"Obsolete ZIP upload remains");
    const toggle=card.getByRole("checkbox");await toggle.check();
    await card.getByLabel("Domain relay",{exact:true}).fill("relay.example.test:"+fixture.control);
    const password=card.getByLabel("Mật khẩu relay",{exact:true});
    const save=card.getByRole("button",{name:"Cập nhật relay",exact:true});
    async function apply(expected) {
      const response=page.waitForResponse(r=>r.url()===url+"/api/relay" && r.request().method()==="PUT");
      await save.click();assert.equal((await response).status(),expected);
    }
    await apply(400);
    await password.fill("wrong-enrollment-password");await apply(400);
    await password.fill("browser-relay-fixture-password");await apply(202);
    assert.equal(await password.inputValue(),"","Password was retained in the form");
    const status=await connected();await exchange(status.remotePort);
    assert.equal(status.domain,"relay.example.test");
    const overview=await (await context.request.get(url+"/api/status")).json();
    assert.equal(overview.clientEndpoint,status.endpoint);
    for(const key of ["password","accessKey","token","ca","profileZip"]) assert(!(key in status));
    const peerResponse=await context.request.post(url+"/api/peers",{data:{name:"Password relay device",clientRoutes:"10.8.0.0/24",dns:"",keepalive:null,mtu:null,expiresAt:null}});
    assert.equal(peerResponse.status(),201);const peer=await peerResponse.json();
    const config=()=>context.request.get(url+"/api/peers/"+peer.id+"/config").then(r=>r.text());
    assert((await config()).includes("Endpoint = "+status.endpoint));
    await stop(server);server=start();await ready();
    const resumed=await connected();assert.equal(resumed.endpoint,status.endpoint);await exchange(status.remotePort);
    await page.reload();await card.getByText(status.endpoint,{exact:true}).waitFor();
    assert.equal(await toggle.isChecked(),true);assert.equal(await password.inputValue(),"");
    await page.setViewportSize({width:390,height:844});await card.scrollIntoViewIfNeeded();
    const box=await card.boundingBox();assert(box && box.x>=0 && box.x+box.width<=391);
    if(process.env.VPN20_UI_SCREENSHOT) await card.screenshot({path:process.env.VPN20_UI_SCREENSHOT});
    await toggle.uncheck();await apply(202);
    assert((await config()).includes("Endpoint = direct.example.test:"+fixture.local));
    await toggle.check();await apply(202);await connected();await exchange(status.remotePort);
    assert.deepEqual(jsErrors,[]);
    console.log("Password enrollment, verified TLS, real embedded UDP, restart, export, disable/re-enable and mobile layout PASS");
  } finally {
    if(browser) await browser.close();
    await stop(server);await stop(vps);fs.rmSync(work,{recursive:true,force:true});
  }
}
main().catch(error=>{console.error(error);process.exitCode=1;});
