import './build.mjs';
import { parseArgs } from 'node:util';
const {values}=parseArgs({options:{state:{type:'string'},port:{type:'string',default:'0'}}});
if(!values.state) throw new Error('Supply --state /tmp/PROTOTYPE-editor/PROTOTYPE-state.json');
const {createHost}=await import('./serve.mjs');
const host=await createHost(values.state);
host.listen(Number(values.port),'127.0.0.1',()=>console.log(`http://127.0.0.1:${host.address().port}`));
