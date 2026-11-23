echo '@@ copy-flute.mid'
rclone-copy operations-copyfile --src-fs 'lab-src:$LAB/source' --src-remote flute.mid --dst-fs lab-compress: --dst-remote flute.mid
echo '@@ copy-flute.musicxml'
rclone-copy operations-copyfile --src-fs 'lab-src:$LAB/source' --src-remote flute.musicxml --dst-fs lab-compress: --dst-remote flute.musicxml
echo '@@ list'
rclone-read operations-list --json '{"fs": "lab-compress:", "remote": "", "opt": "{\"recurse\": true, \"showHash\": true}"}'
echo '@@ stat-flute.mid'
rclone-read operations-stat --fs lab-compress: --remote flute.mid
echo '@@ sha256-flute.mid'
rclone-read operations-hashsumfile --fs lab-compress: --remote flute.mid --hash-type sha256 --download
echo '@@ stat-flute.musicxml'
rclone-read operations-stat --fs lab-compress: --remote flute.musicxml
echo '@@ sha256-flute.musicxml'
rclone-read operations-hashsumfile --fs lab-compress: --remote flute.musicxml --hash-type sha256 --download
echo '@@ size'
rclone-read operations-size --fs lab-compress:
echo '@@ check'
rclone-read operations-check --src-fs 'lab-src:$LAB/source' --dst-fs lab-compress: --download --one-way --match --combined
