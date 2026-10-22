import { useEffect, useState, type FormEvent } from "react";
import { api, type SubdomainStatus } from "../api";
import { errorMessage } from "../state";

export function Subdomain() {
 const [status,setStatus]=useState<SubdomainStatus|null>(null);
 const [name,setName]=useState("");
 const [error,setError]=useState("");
 const [loadError,setLoadError]=useState("");
 const [sending,setSending]=useState(false);
 useEffect(()=>{
  let alive=true;
  let initialized=false;
  const refresh=()=>api.subdomain().then(s=>{
   if (!alive) return;
   setStatus(s);
   setLoadError("");
   if(!initialized){setName(s.name);initialized=true;}
  }).catch(e=>{if(alive)setLoadError(errorMessage(e));});
  void refresh();
  const timer=setInterval(refresh,3000);
  return ()=>{alive=false;clearInterval(timer);};
 },[]);
 async function submit(e:FormEvent){
  e.preventDefault();setError("");setSending(true);
  try{setStatus(await api.updateSubdomain(name.trim().toLowerCase()));}
  catch(e){setError(errorMessage(e));}
  finally{setSending(false);}
 }
 return <form className="card" onSubmit={submit} style={{marginTop:20}}>
  <div className="card-body">
   <label htmlFor="subdomain-name" style={{display:"block",marginBottom:6,fontSize:13,fontWeight:600}}>Subdomain</label>
   <div style={{display:"flex",alignItems:"center",gap:8,flexWrap:"wrap"}}>
    <div style={{display:"flex",alignItems:"center",gap:4,flex:"1 1 240px"}}>
     <input id="subdomain-name" aria-label="Tên subdomain" className="input" value={name}
      onChange={e=>setName(e.target.value)} placeholder="vanphong" required maxLength={63}
      pattern={"[a-zA-Z0-9]([a-zA-Z0-9\\-]{0,61}[a-zA-Z0-9])?"}
      autoCapitalize="none" autoCorrect="off" spellCheck={false}
      disabled={sending||status?.busy} style={{minWidth:90}}/>
     <span style={{whiteSpace:"nowrap"}}>.vpn.20.com.vn</span>
    </div>
    <button className="btn primary" type="submit" disabled={!status||sending||status.busy}>{sending||status?.busy?"Đang cập nhật…":"Cập nhật"}</button>
   </div>
   <div role="status" aria-live="polite" style={{marginTop:8,fontSize:13}}>
    {error||loadError||status?.message||"Nhập tên để bật DDNS và SSL tự động."}
    {status?.sslReady&&status.url&&<> · <a href={status.url} target="_blank" rel="noreferrer">{status.url}</a></>}
   </div>
  </div>
 </form>;
}
