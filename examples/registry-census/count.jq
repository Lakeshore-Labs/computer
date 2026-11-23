reduce .servers[].server as $server (
  {remote_only: 0, package_only: 0, both: 0, neither: 0, rows: 0};
  (($server.remotes | length) > 0) as $remote |
  (($server.packages | length) > 0) as $package |
  .rows += 1 |
  if $remote and $package then .both += 1
  elif $remote then .remote_only += 1
  elif $package then .package_only += 1
  else .neither += 1 end
)
