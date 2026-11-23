# Runs INSIDE stock Computer (WASI CPython). Producer role: uses Computer's generated Python bindings
# (tools.nats -> upstream mcp-nats over stdio): nats_obj_create_bucket, nats_obj_put, nats_stream_publish.
# Everything the guest needs is under /work (mounted). No network, no host paths.
import base64, datetime, hashlib, json, math, struct, wave
from tools import nats

R = 8000
NOTES = [(440.0, 0.4), (523.25, 0.4), (659.25, 0.4), ((440.0, 523.25), 0.3)]
def make(path):
    fr = []
    for f, d in NOTES:
        n = int(R * d); fs = f if isinstance(f, tuple) else (f,)
        for i in range(n):
            env = min(1, i / 80, (n - i) / 160)
            fr.append(int(14000 * env * sum(math.sin(2 * math.pi * x * i / R) for x in fs) / len(fs)))
    with wave.open(path, 'wb') as w:
        w.setnchannels(1); w.setsampwidth(2); w.setframerate(R); w.writeframes(struct.pack('<%dh' % len(fr), *fr))

make('/work/cue-guest.wav')
wav = open('/work/cue-guest.wav', 'rb').read()          # guest reads its own WAV
sha = hashlib.sha256(wav).hexdigest()
b64 = base64.b64encode(wav).decode('ascii')              # obj_put takes a string -> store ASCII base64
calls = []
def rec(tool, args, result):
    short = {k: (v if len(str(v)) <= 60 else '<%d chars>' % len(str(v))) for k, v in args.items()}
    calls.append({'binding': 'tools.nats.' + tool, 'args': short, 'result': result})
    return result

a = dict(bucket='CUES', description='station cues (base64 text of PCM16 WAV)', storage='file')
rec('nats_obj_create_bucket', a, nats.nats_obj_create_bucket(**a))
a = dict(bucket='CUES', name='stinger-v2.wav.b64', data=b64, description='encoding=base64(ascii) of audio/wav pcm16 8000Hz mono')
rec('nats_obj_put', a, nats.nats_obj_put(**a))
order = {'rev': 2, 'amended': True, 'show': 'Community radio synthetic 18:00 block',
         'cue': {'bucket': 'CUES', 'name': 'stinger-v2.wav.b64', 'stored_encoding': 'base64-ascii-in-utf8',
                 'decoded_bytes': len(wav), 'sha256': sha, 'expected_duration_ms': 1500},
         'items': [{'t': '18:29:30.000', 'id': 'weather', 'len_s': 28.0},
                   {'t': '18:29:58.000', 'id': 'STINGER-v2', 'slot_ms': 2000},
                   {'t': '18:30:00.000', 'id': 'news-top', 'len_s': 300}]}
t1 = datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='milliseconds').replace('+00:00', 'Z')
a = dict(subject='desk.rundown.amend', payload=json.dumps(order))
rec('nats_stream_publish', a, nats.nats_stream_publish(**a))
proof = {'role': 'producer', 'runtime': 'stock Computer WASI Python', 'T1_amendment_published': t1,
         'wav_bytes': len(wav), 'b64_chars': len(b64), 'sha256': sha, 'calls': calls}
json.dump(proof, open('/work/producer-proof.json', 'w'), indent=1)
print('producer ok', t1, sha[:12])
