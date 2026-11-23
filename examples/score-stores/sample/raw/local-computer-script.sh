echo '@@ copy-flute.mid'
rclone-copy operations-copyfile --src-fs 'lab-src:$LAB/source' --src-remote flute.mid --dst-fs 'lab-local:$LAB/roots/local-dest' --dst-remote flute.mid
echo '@@ copy-flute.musicxml'
rclone-copy operations-copyfile --src-fs 'lab-src:$LAB/source' --src-remote flute.musicxml --dst-fs 'lab-local:$LAB/roots/local-dest' --dst-remote flute.musicxml
echo '@@ list'
rclone-read operations-list --json '{"fs": "lab-local:$LAB/roots/local-dest", "remote": "", "opt": "{\"recurse\": true, \"showHash\": true}"}'
echo '@@ stat-flute.mid'
rclone-read operations-stat --fs 'lab-local:$LAB/roots/local-dest' --remote flute.mid
echo '@@ sha256-flute.mid'
rclone-read operations-hashsumfile --fs 'lab-local:$LAB/roots/local-dest' --remote flute.mid --hash-type sha256 --download
echo '@@ stat-flute.musicxml'
rclone-read operations-stat --fs 'lab-local:$LAB/roots/local-dest' --remote flute.musicxml
echo '@@ sha256-flute.musicxml'
rclone-read operations-hashsumfile --fs 'lab-local:$LAB/roots/local-dest' --remote flute.musicxml --hash-type sha256 --download
echo '@@ size'
rclone-read operations-size --fs 'lab-local:$LAB/roots/local-dest'
echo '@@ check'
rclone-read operations-check --src-fs 'lab-src:$LAB/source' --dst-fs 'lab-local:$LAB/roots/local-dest' --download --one-way --match --combined
