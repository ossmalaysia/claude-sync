package browser

import (
	"encoding/json"
	"fmt"
)

// jsString returns s as a safely quoted JavaScript string literal.
func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// hostGuard makes every snippet return {host} without fetching when the tab
// is not on claude.ai (for example on an SSO provider during login), so no
// request is ever sent to the wrong host.
const hostGuard = `if(location.host!=="claude.ai"){return {status:0,host:location.host}}`

func jsJSON(method, url string, body []byte) string {
	b := "undefined"
	if body != nil {
		b = jsString(string(body))
	}
	return fmt.Sprintf(`(async()=>{`+hostGuard+`const r=await fetch(%s,{method:%s,credentials:"include",headers:{"content-type":"application/json"},body:%s});return {status:r.status,body:await r.text()}})()`,
		jsString(url), jsString(method), b)
}

// jsDownload returns the response body base64-encoded, in chunks to avoid
// call-stack limits on large files.
func jsDownload(url string) string {
	return fmt.Sprintf(`(async()=>{`+hostGuard+`const r=await fetch(%s,{credentials:"include"});const b=new Uint8Array(await r.arrayBuffer());let s="";for(let i=0;i<b.length;i+=32768){s+=String.fromCharCode.apply(null,b.subarray(i,i+32768));}return {status:r.status,b64:btoa(s)}})()`,
		jsString(url))
}

// jsUpload posts multipart field "file", plus any extra text fields, as the
// claude.ai web app does.
func jsUpload(url, fileName, mime, b64 string, fields map[string]string) string {
	extra, _ := json.Marshal(fields) // null when fields is nil
	return fmt.Sprintf(`(async()=>{`+hostGuard+`const bin=atob(%s);const bytes=new Uint8Array(bin.length);for(let i=0;i<bin.length;i++){bytes[i]=bin.charCodeAt(i);}const fd=new FormData();fd.append("file",new Blob([bytes],{type:%s}),%s);const extra=%s||{};for(const k of Object.keys(extra)){fd.append(k,extra[k]);}const r=await fetch(%s,{method:"POST",body:fd,credentials:"include"});return {status:r.status,body:await r.text()}})()`,
		jsString(b64), jsString(mime), jsString(fileName), string(extra), jsString(url))
}
