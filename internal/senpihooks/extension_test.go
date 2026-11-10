package senpihooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExtensionRoutesSettledAndBlockedEvents(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required to execute the Senpi extension contract")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-notify.mjs")
	if err := Install(path, "C:/通知/agent-notify.exe", "C:/配置/config.yaml"); err != nil {
		t.Fatal(err)
	}
	testPath := filepath.Join(dir, "verify.mjs")
	script := `import {readFileSync} from 'node:fs';
import vm from 'node:vm';
import assert from 'node:assert/strict';
const calls=[];
const context=vm.createContext({process:{cwd:()=>'/fallback'}});
const childModule=new vm.SyntheticModule(['spawn'],function(){this.setExport('spawn',(binary,args)=>({
  on(){}, unref(){}, stdin:{on(){},end(data){calls.push({binary,args:[...args],payload:JSON.parse(data)})}}
}));},{context});
const mod=new vm.SourceTextModule(readFileSync(process.argv[2],'utf8'),{context});
await mod.link(()=>childModule);await mod.evaluate();
const handlers=new Map(),bus=new Map();
const pi={cwd:'/项目',sessionKind:'interactive',on:(e,f)=>handlers.set(e,f),events:{on:(e,f)=>bus.set(e,f)}};
mod.namespace.default(pi);
const ctx={sessionManager:{getSessionId:()=> 's1'}};
handlers.get('session_start')({},ctx);
bus.get('herdr:blocked')({active:true,kind:'permission',label:'授权'});
bus.get('herdr:blocked')({active:false,kind:'permission'});
bus.get('herdr:blocked')({active:true,kind:'question',label:'输入'});
assert.equal(calls.length,2);
assert.equal(calls[0].payload.event,'permission_required');
assert.equal(calls[1].payload.event,'input_required');
assert.equal(calls[0].payload.session_id,'s1');
handlers.get('agent_start')({},ctx);
handlers.get('agent_end')({messages:[{role:'assistant',stopReason:'error'}]},ctx);
assert.equal(calls.length,2);
handlers.get('agent_settled')({},ctx);
assert.equal(calls.at(-1).payload.event,'run_failed');
handlers.get('agent_start')({},ctx);
handlers.get('agent_end')({messages:[{role:'assistant',stopReason:'stop'}]},ctx);
handlers.get('agent_settled')({},ctx);
assert.equal(calls.at(-1).payload.event,'run_completed');
handlers.get('agent_end')({messages:[{role:'assistant',stopReason:'aborted'}]},ctx);
handlers.get('agent_settled')({},ctx);
assert.equal(calls.length,4);
pi.sessionKind='worker';handlers.get('agent_settled')({},ctx);assert.equal(calls.length,4);
assert.equal(handlers.has('ui_prompt_start'),false);
assert.equal(calls[0].binary,'C:/通知/agent-notify.exe');
assert.deepEqual(calls[0].args,['handle-omo-hook','--config','C:/配置/config.yaml']);
assert.equal(calls[0].payload.cwd,'/项目');
`
	if err := os.WriteFile(testPath, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--experimental-vm-modules", testPath, path).CombinedOutput(); err != nil {
		t.Fatalf("extension contract: %v\n%s", err, out)
	}
}

func TestInstallAndUninstallProtectUnrelatedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := []byte(`{"model":"keep"}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Install(path, "/bin/agent-notify", ""); err == nil {
		t.Fatal("overwrote unrelated file")
	}
	if err := Uninstall(path); err == nil {
		t.Fatal("deleted unrelated file")
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(original) {
		t.Fatal("unrelated file changed")
	}
}
