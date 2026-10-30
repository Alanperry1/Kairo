const req=JSON.parse(require('fs').readFileSync(0,'utf8'));
const sum=req.inputs.a+Number(req.dataset_rows[0].value);
console.log(JSON.stringify({kind:'computational_experiment',metrics:{sum,secret_present:!!process.env.API_TOKEN,uid:process.getuid()},findings:['Computed sum '+sum],limitations:['Test computation only']}));
