// Node.js 22+. Submission-only client; never approves or executes deployment.
const token=process.env.STAKRAF_PIPELINE_TOKEN;
const image=process.env.STAKRAF_RELEASE_IMAGE;
const releaseID=process.env.STAKRAF_RELEASE_ID||(process.env.GITHUB_RUN_ID&&process.env.GITHUB_RUN_ATTEMPT?`github-${process.env.GITHUB_RUN_ID}-${process.env.GITHUB_RUN_ATTEMPT}`:'');
const url=new URL('/api/pipeline/releases',process.env.STAKRAF_URL||'https://console.stakraf.com');
if(url.protocol!=='https:'||url.username||url.password)throw new Error('STAKRAF_URL must use HTTPS without URL credentials.');
if(!token?.startsWith('sci_')||!image)throw new Error('Pipeline token and immutable release image are required.');
if(!releaseID)throw new Error('Set STAKRAF_RELEASE_ID or run from GitHub Actions with a run id and attempt.');
const response=await fetch(url,{
 method:'POST',redirect:'error',signal:AbortSignal.timeout(30000),
 headers:{'Content-Type':'application/json',Authorization:'Bearer '+token},
 body:JSON.stringify({image,commit_sha:process.env.GITHUB_SHA||'',note:process.env.STAKRAF_RELEASE_NOTE||'Submitted by CI',request_id:releaseID}),
});
if(!response.ok)throw new Error('Stakraf release submission failed with HTTP '+response.status);
const result=await response.json();
console.log(JSON.stringify({release_id:result.id,status:result.status,approval_required:result.approval_required}));
