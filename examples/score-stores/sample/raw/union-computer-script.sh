echo '@@ copy-flute.mid'
rclone-copy operations-copyfile --src-fs 'lab-src:$LAB/source' --src-remote flute.mid --dst-fs lab-union: --dst-remote flute.mid
echo '@@ copy-flute.musicxml'
rclone-copy operations-copyfile --src-fs 'lab-src:$LAB/source' --src-remote flute.musicxml --dst-fs lab-union: --dst-remote flute.musicxml
echo '@@ list'
rclone-read operations-list --json '{"fs": "lab-union:", "remote": "", "opt": "{\"recurse\": true, \"showHash\": true}"}'
echo '@@ stat-flute.mid'
rclone-read operations-stat --fs lab-union: --remote flute.mid
echo '@@ sha256-flute.mid'
rclone-read operations-hashsumfile --fs lab-union: --remote flute.mid --hash-type sha256 --download
echo '@@ stat-flute.musicxml'
rclone-read operations-stat --fs lab-union: --remote flute.musicxml
echo '@@ sha256-flute.musicxml'
rclone-read operations-hashsumfile --fs lab-union: --remote flute.musicxml --hash-type sha256 --download
echo '@@ size'
rclone-read operations-size --fs lab-union:
echo '@@ check'
rclone-read operations-check --src-fs 'lab-src:$LAB/source' --dst-fs lab-union: --download --one-way --match --combined
