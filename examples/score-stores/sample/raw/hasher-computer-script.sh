echo '@@ copy-flute.mid'
rclone-copy operations-copyfile --src-fs 'lab-src:$LAB/source' --src-remote flute.mid --dst-fs lab-hasher: --dst-remote flute.mid
echo '@@ copy-flute.musicxml'
rclone-copy operations-copyfile --src-fs 'lab-src:$LAB/source' --src-remote flute.musicxml --dst-fs lab-hasher: --dst-remote flute.musicxml
echo '@@ list'
rclone-read operations-list --json '{"fs": "lab-hasher:", "remote": "", "opt": "{\"recurse\": true, \"showHash\": true}"}'
echo '@@ stat-flute.mid'
rclone-read operations-stat --fs lab-hasher: --remote flute.mid
echo '@@ sha256-flute.mid'
rclone-read operations-hashsumfile --fs lab-hasher: --remote flute.mid --hash-type sha256 --download
echo '@@ stat-flute.musicxml'
rclone-read operations-stat --fs lab-hasher: --remote flute.musicxml
echo '@@ sha256-flute.musicxml'
rclone-read operations-hashsumfile --fs lab-hasher: --remote flute.musicxml --hash-type sha256 --download
echo '@@ size'
rclone-read operations-size --fs lab-hasher:
echo '@@ check'
rclone-read operations-check --src-fs 'lab-src:$LAB/source' --dst-fs lab-hasher: --download --one-way --match --combined
