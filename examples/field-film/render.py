"""Assemble the film from the published examples and a local Piper voice."""
import argparse
import base64
import hashlib
import json
import re
import subprocess
import tempfile
import wave
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont
from chapters import chapter_metadata
from piper import PiperVoice, SynthesisConfig

HERE = Path(__file__).resolve().parent
FPS = 24
WIDTH, HEIGHT = 1280, 720
VOICE_SHA = '5d4f08ba6a2a48c44592eed3ce56bf85e9de3dd4e20df90541ae68a8310c029a'
CONFIG_SHA = '7e1f4634af596d83cca997fb7a931ba80b70f8a316a2655ee69c55365e0ace14'


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def ffmpeg(*args):
    subprocess.run(['ffmpeg', '-loglevel', 'error', '-y', *map(str, args)], check=True)


def lines(draw, text, font, width):
    result = ['']
    for word in text.split():
        candidate = (result[-1] + ' ' + word).strip()
        if draw.textlength(candidate, font=font) > width and result[-1]:
            result.append(word)
        else:
            result[-1] = candidate
    return result


def frame(scene, caption, number, total, fontfile, boldfile, target):
    layer = Image.new('RGBA', (WIDTH, HEIGHT), '#f6f2e9')
    draw = ImageDraw.Draw(layer)
    draw.rectangle((24, 64, 933, 576), fill=(0, 0, 0, 0))
    ink, accent = '#182c31', '#95502b'
    def label(x, y, value, size, color, bold=False, width=None):
        font = ImageFont.truetype(str(boldfile if bold else fontfile), size)
        for n, line in enumerate(lines(draw, value, font, width) if width else [value]):
            draw.text((x, y + n * int(size * 1.27)), line, fill=color, font=font)
    label(24, 16, 'Computer   /   Field guide', 24, ink, True)
    label(955, 18, f'CHAPTER   {number}  /  {total}', 18, accent, True)
    label(955, 106, scene['title'], 36, ink, True, 298)
    label(955, 270, scene['figure'], 51, accent, True)
    label(957, 338, scene['unit'], 23, ink, False, 290)
    label(957, 473, scene['credit'], 17, ink, False, 285)
    draw.line((24, 597, 1256, 597), fill='#afbcad', width=2)
    label(31, 615, caption, 26, ink, width=1198)
    layer.save(target)


def ts(t):
    millis = round(t * 1000)
    return f'{millis//3600000:02}:{millis//60000%60:02}:{millis//1000%60:02}.{millis%1000:03}'


def render(args, tmp):
    config_path = Path(str(args.voice) + '.json')
    if (sha(args.voice), sha(config_path)) != (VOICE_SHA, CONFIG_SHA):
        raise ValueError('Use the pinned LJ Speech High model and config.')
    voice = PiperVoice.load(str(args.voice), config_path=str(config_path))
    scenes = json.loads(args.scenes.read_text())
    captions, transcript, clips, chapters = ['WEBVTT\n'], [], [], []
    start_frame = 0
    for number, scene in enumerate(scenes, 1):
        chapter = {'title': scene['title'], 'source': scene['source'], 'start': start_frame / FPS, 'shots': []}
        transcript.append('## ' + scene['title'] + '\n')
        for shot in scene['shots']:
            name, sentence, *extras = shot
            source = HERE.parent / scene['source'] / scene.get('folder', 'sample') / name
            speech, overlay, clip = (tmp / f'{len(clips):02}.{ext}' for ext in ('wav', 'png', 'mp4'))
            with wave.open(str(speech), 'wb') as audio:
                voice.synthesize_wav(sentence, audio, SynthesisConfig(length_scale=.91))
            with wave.open(str(speech)) as audio:
                speech_duration = audio.getnframes() / audio.getframerate()
            music_time = 5.2 if extras else 0
            sound_caption = '[Opening phrase of Last Boat Home, synthesized flute]'
            cue = None
            if extras and isinstance(extras[0], dict):
                cue = source.parent / extras[0]['file']
                sound_caption = extras[0]['caption']
                with wave.open(str(cue)) as audio:
                    music_time = audio.getnframes() / audio.getframerate()
            frames = round((speech_duration + .34 + music_time) * FPS)
            duration = frames / FPS
            frame(scene, sentence, number, len(scenes), args.font, args.bold, overlay)
            is_still = source.suffix == '.png'
            visual = ['-loop', '1', '-framerate', str(FPS)] if is_still else []
            inputs = [*visual, '-i', source, '-i', overlay, '-i', speech]
            sound = '[2:a]aresample=44100,apad[a]'
            if music_time:
                if cue is not None:
                    music = cue
                else:
                    stand = source.with_name('stand.html').read_text()
                    assignment = re.search(r"const DATA\s*=\s*", stand)
                    score, _ = json.JSONDecoder().raw_decode(stand[assignment.end():])
                    flute = score["parts"]["flute"]
                    music = tmp / 'opening.wav'
                    music.write_bytes(base64.b64decode(flute["b64"]["wav"], validate=True))
                    if sha(music) != flute["sha_wav"]:
                        raise ValueError("The embedded flute audio does not match the rehearsal reader.")
                inputs.extend(['-i', music])
                sound = (f'[3:a]atrim=duration={music_time},aresample=44100,afade=t=out:st={max(0,music_time-.4)}:d=0.4,'
                         f'adelay={round((speech_duration + .18)*1000)}:all=1[m];'
                         '[2:a]aresample=44100,apad[v];[v][m]amix=inputs=2:normalize=0[a]')
            picture = ('[0:v]fps=24,scale=910:512:force_original_aspect_ratio=decrease:force_divisible_by=2,'
                       'pad=910:512:(ow-iw)/2:(oh-ih)/2:color=0x243339,setsar=1,'
                       'tpad=stop_mode=clone:stop_duration=60,pad=1280:720:24:64[v];[v][1:v]overlay[out]')
            ffmpeg(*inputs, '-filter_complex_threads', 2, '-filter_complex', picture + ';' + sound,
                   '-map', '[out]', '-map', '[a]', '-t', duration, '-r', FPS, '-c:v', 'libx264',
                   '-threads', 3, '-preset', 'veryfast', '-crf', 19, '-pix_fmt', 'yuv420p',
                   '-c:a', 'aac', '-ar', 44100, '-ac', 2, clip)
            start = start_frame / FPS
            start_frame += frames
            captions.append(f'{ts(start)} --> {ts(start + speech_duration)}\n{sentence}\n')
            transcript.append(sentence + '\n')
            if music_time:
                captions.append(f'{ts(start + speech_duration + .18)} --> {ts(start_frame / FPS)}\n{sound_caption}\n')
                transcript.append(sound_caption+'\n')
            chapter['shots'].append({'file': name, 'sha256': sha(source), 'start': start, 'frames': frames})
            clips.append(clip)
        chapters.append(chapter)
    args.out.mkdir(parents=True, exist_ok=True)
    (tmp / 'clips').write_text(''.join("file '" + str(p) + "'\n" for p in clips))
    vtt = args.out / 'captions.vtt'
    vtt.write_text('\n'.join(captions))
    metadata = tmp / 'chapters.ffmeta'
    metadata.write_text(chapter_metadata(chapters, FPS))
    ffmpeg('-f', 'concat', '-safe', 0, '-i', tmp / 'clips', '-i', vtt, '-f', 'ffmetadata', '-i', metadata, '-map_metadata', 2, '-map_chapters', 2, '-map', '0:v', '-map', '0:a',
           '-map', '1:s', '-c:v', 'copy', '-c:a', 'copy', '-c:s', 'mov_text', '-metadata:s:s:0',
           'language=eng', '-metadata:s:a:0', 'language=eng', '-movflags', '+faststart', args.out / 'computer.mp4')
    ffmpeg('-ss', 1, '-i', args.out / 'computer.mp4', '-frames:v', 1, args.out / 'poster.jpg')
    (args.out / 'chapters.json').write_text(json.dumps(chapters, indent=2) + '\n')
    (args.out / 'transcript.md').write_text('# ' + args.title + '\n\n' + '\n'.join(transcript))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--scenes', type=Path, default=HERE / 'scenes.json')
    parser.add_argument('--title', default='Computer field guide')
    for opt in ('voice', 'font', 'bold', 'out'):
        parser.add_argument('--' + opt, type=Path, required=True)
    with tempfile.TemporaryDirectory() as temp:
        render(parser.parse_args(), Path(temp))
