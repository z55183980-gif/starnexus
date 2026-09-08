import paramiko
k=r'C:\Users\Administrator\.ssh\starnexus_ops_ed25519'
c=paramiko.SSHClient(); c.set_missing_host_key_policy(paramiko.AutoAddPolicy()); c.connect('67.230.183.84',username='root',key_filename=k,timeout=20,look_for_keys=False,allow_agent=False)
queries={
'rows':"""select to_timestamp(created_at) at time zone 'UTC' as utc,id,type,model_name,channel_id,channel_name,use_time_ms,prompt_tokens,completion_tokens,request_id,upstream_request_id,upstream_account_id,other::json->'admin_info'->>'node_name',other::json->'stream_status',other::json->'admin_info'->>'request_path',content from logs where user_id=57 and created_at between extract(epoch from timestamp '2026-09-08 06:08:00') and extract(epoch from timestamp '2026-09-08 06:15:59') order by created_at,id""",
'counts':"""select count(*),count(*) filter(where model_name='gpt-5.6-sol'),count(*) filter(where other::json->'stream_status'->>'status'='ok'),count(*) filter(where other::json->'stream_status'->>'status'='error'),min(to_timestamp(created_at) at time zone 'UTC'),max(to_timestamp(created_at) at time zone 'UTC') from logs where user_id=57 and created_at between extract(epoch from timestamp '2026-09-08 06:08:00') and extract(epoch from timestamp '2026-09-08 06:15:59')""",
'errors':"""select to_timestamp(created_at) at time zone 'UTC',request_id,model_name,other::json->'stream_status',other::json->'admin_info'->>'node_name',content from logs where user_id=57 and created_at between extract(epoch from timestamp '2026-09-08 06:08:00') and extract(epoch from timestamp '2026-09-08 06:15:59') and (other::json->'stream_status'->>'status' <> 'ok' or content <> '') order by created_at""",
'user':"select id,username,email from users where id=57"
}
for name,q in queries.items():
 import base64
 enc=base64.b64encode(q.encode()).decode()
 cmd=f"echo {enc} | base64 -d | docker exec -i db-postgres psql -U starNex -d starnex -P pager=off -At -F '|'"
 _,o,e=c.exec_command(cmd,timeout=180)
 print('===%s===\\n'%name+o.read().decode(errors='replace')); print(e.read().decode(errors='replace'))
c.close()

