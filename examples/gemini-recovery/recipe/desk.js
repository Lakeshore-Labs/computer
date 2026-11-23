const receipt = JSON.parse(document.getElementById('receipt').textContent);
const rows = receipt.events;
const names = {opened:'The desk opens', packing:'Packing begins', interrupted:'Process stops', reopened:'Conversation reopens', finished:'Notice issued'};
const timeline = document.getElementById('timeline');
let timer;
const buttons = rows.map((row,i)=>{
  const item=document.createElement('li');
  const button=document.createElement('button');
  const label=document.createElement('span');
  label.textContent=names[row.step];
  const time=document.createElement('time');
  time.textContent=row.elapsed_s.toFixed(2)+' s';
  button.append(label,time);item.append(button);timeline.append(item);
  button.onclick=()=>{clearTimeout(timer);show(i);};
  return button;
});
function show(i) {
  const row=rows[i];
  buttons.forEach((button,n)=>button.setAttribute('aria-current',String(i===n)));
  document.getElementById('tag').textContent='Claim LF-042 · '+row.elapsed_s.toFixed(2)+' s';
  document.getElementById('step').textContent=names[row.step];
  const values={
    opened:['Computer launched the standalone agent with a blank conversation directory.','Process '+row.child_process,'The first instruction supplied a pickup word.'],
    packing:['The tool had written its in-progress file. The packed file was still absent.',row.in_progress,row.replay_markers+' persisted provider marker before interruption.'],
    interrupted:['The host stopped the child it had launched during the tool call.','Exit '+row.exit,'Terminal events before interruption: '+(row.terminal_events?.length||0)+'.'],
    reopened:['A new Computer process continued the same conversation.','Process '+row.child_process,'The reopened desk allowed the idempotent packing command to finish.'],
    finished:['The second process wrote this exact synthetic notice.',row.notice,row.conversations+' conversation directory. Terminal event: '+(row.terminal_events||[]).join(', ')+'.']
  }[row.step];
  for(const [id,value] of [['detail',values[0]],['value',values[1]],['context',values[2]]]) {
    document.getElementById(id).textContent=value;
  }
}
function play(i){show(i);if(i<rows.length-1)timer=setTimeout(()=>play(i+1),1000*(rows[i+1].elapsed_s-rows[i].elapsed_s));}
document.getElementById('play').onclick=()=>{clearTimeout(timer);play(0);};
show(rows.length-1);
