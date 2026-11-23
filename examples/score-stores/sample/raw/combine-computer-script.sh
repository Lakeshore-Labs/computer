echo '@@ copy-flute.mid'
rclone-copy operations-copyfile --src-fs 'lab-src:$LAB/source' --src-remote flute.mid --dst-fs lab-combine: --dst-remote part-midi/flute.mid
echo '@@ copy-flute.musicxml'
rclone-copy operations-copyfile --src-fs 'lab-src:$LAB/source' --src-remote flute.musicxml --dst-fs lab-combine: --dst-remote part-xml/flute.musicxml
echo '@@ list'
rclone-read operations-list --json '{"fs": "lab-combine:", "remote": "", "opt": "{\"recurse\": true, \"showHash\": true}"}'
echo '@@ stat-flute.mid'
rclone-read operations-stat --fs lab-combine: --remote part-midi/flute.mid
echo '@@ sha256-flute.mid'
rclone-read operations-hashsumfile --fs lab-combine: --remote part-midi/flute.mid --hash-type sha256 --download
echo '@@ stat-flute.musicxml'
rclone-read operations-stat --fs lab-combine: --remote part-xml/flute.musicxml
echo '@@ sha256-flute.musicxml'
rclone-read operations-hashsumfile --fs lab-combine: --remote part-xml/flute.musicxml --hash-type sha256 --download
echo '@@ size'
rclone-read operations-size --fs lab-combine:
