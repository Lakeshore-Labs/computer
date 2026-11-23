# frozen_string_literal: true
# Independent Ruby consumer of the Postcard Press MCP host, using the official `mcp` gem.
# Usage: bundle exec ruby docent_tour.rb ENDPOINT OUTDIR   (ENDPOINT like http://127.0.0.1:PORT)
require "bundler/setup"
require "mcp"
require "json"
require "digest"
require "zlib"
require "rubygems/package"
require "net/http"
require "open3"

endpoint, outdir = ARGV
abort "usage: docent_tour.rb ENDPOINT OUTDIR" unless endpoint && outdir
Dir.mkdir(outdir)
Dir.mkdir(File.join(outdir, "cards"))
sha = ->(s) { Digest::SHA256.hexdigest(s) }

transport = MCP::Client::HTTP.new(url: endpoint.chomp("/") + "/mcp")
client = MCP::Client.new(transport: transport)
client.connect(client_info: { name: "ruby-docent-consumer", version: "1.0" })
tools = client.tools.map(&:name).sort
abort "expected 4 tools, got #{tools}" unless tools == %w[bash edit read write]

trace = []
call = lambda do |name, args|
  r = client.call_tool(name: name, arguments: args)
  res = r.fetch("result", r)
  text = res["content"].map { |c| c["text"] }.join
  abort "#{name} failed: #{text[0, 200]}" if res["isError"]
  trace << { tool: name, path: args[:path] || args["path"], bytes: text.bytesize }
  text
end

# 1. Read-only catalog records from the real mounted collection.
getty   = JSON.parse(call.("read", { path: "/collection/getty/record.json" }))
year    = JSON.parse(call.("read", { path: "/collection/getty/year.json" }))
cleve   = JSON.parse(call.("read", { path: "/collection/cleveland/record.json" }))["data"]
getty_acc = getty["metadata"].find { |f| f["label"]["en"] == ["Accession Number"] }["value"]["en"][0]
catalog = {
  "getty"     => { title: getty["label"]["en"][0], accession: getty_acc, year: year.to_s, artist: getty["metadata"].find { |f| f["label"]["en"] == ["Artist/Maker"] }.fetch("value").fetch("en").first, credit: "The J. Paul Getty Museum" },
  "cleveland" => { title: cleve["title"], accession: cleve["accession_number"], year: cleve["creation_date"], artist: cleve["creators"][0]["description"], credit: "The Cleveland Museum of Art", license: cleve["share_license_status"] }
}

# 2. Docent batch: three personalized cards (the mount has two artworks, so Irises gets two editions).
batch = [
  { museum: "getty",     visitor: "Mira",  edition: "A" },
  { museum: "getty",     visitor: "Tobias", edition: "B" },
  { museum: "cleveland", visitor: "Ines",  edition: "A" }
]
last_order = nil
cards = batch.map.with_index(1) do |b, i|
  rec = catalog[b[:museum]]
  inscription = "For #{b[:visitor]} · #{rec[:accession]} · #{rec[:year]}"
  abort "inscription too long" if inscription.length > 48
  order = JSON.generate({ museum: b[:museum], inscription: inscription })
  if last_order
    call.("edit", { path: "/desk/order.json", oldText: last_order, newText: order })
  else
    call.("write", { path: "/desk/order.json", content: order })
  end
  last_order = order
  call.("bash", { command: "set -eo pipefail\npython3 /recipe/postcard.py" })
  svg = call.("read", { path: "/desk/postcard.svg" })
  receipt = JSON.parse(call.("read", { path: "/desk/receipt.json" }))
  abort "svg hash mismatch" unless sha.(svg) == receipt["svg_sha256"] && svg.bytesize == receipt["svg_bytes"]
  abort "accession not from record" unless receipt["accession"] == rec[:accession] && receipt["title"] == rec[:title]
  abort "inscription not echoed" unless receipt["inscription"] == inscription && svg.include?(inscription)
  name = format("%02d-%s-%s-%s.svg", i, b[:museum], b[:edition].downcase, b[:visitor].downcase)
  File.binwrite(File.join(outdir, "cards", name), svg)
  # Independent native reader: librsvg.
  png = File.join(outdir, "cards", name.sub(".svg", ".png"))
  _, err, st = Open3.capture3("rsvg-convert", "-w", "888", "-h", "630", "-o", png, File.join(outdir, "cards", name))
  abort "rsvg-convert: #{err}" unless st.success?
  { file: name, visitor: b[:visitor], edition: b[:edition], museum: b[:museum], title: rec[:title], accession: rec[:accession],
    credit: rec[:credit], inscription: inscription, svg_bytes: svg.bytesize, svg_sha256: sha.(svg),
    png_sha256: sha.(File.binread(png)), host_receipt: receipt }
end
abort "editions not distinct" unless cards.map { |c| c[:svg_sha256] }.uniq.size == 3
abort "Irises editions not distinct" unless cards[0][:inscription] != cards[1][:inscription]

# 3. Shared-process UI is served (read-only GET; no browser state touched).
ui = Net::HTTP.get_response(URI(endpoint.chomp("/") + "/"))

operator = {
  sdk: "mcp #{MCP::VERSION} (official Ruby SDK)", ruby: RUBY_VERSION, tools: tools,
  catalog_records_read: %w[/collection/getty/record.json /collection/getty/year.json /collection/cleveland/record.json],
  catalog: catalog, cards: cards.map { |c| c.reject { |k, _| k == :host_receipt } },
  ui_http_status: ui.code.to_i, mcp_calls: trace.size, mcp_trace: trace
}
File.write(File.join(outdir, "operator-receipt.json"), JSON.pretty_generate(operator) + "\n")
File.write(File.join(outdir, "host-receipts.json"), JSON.pretty_generate(cards.map { |c| c[:host_receipt] }) + "\n")

# 4. Transferable tour pack: deterministic tar.gz via stdlib (rubygems tar + zlib).
pack = File.join(outdir, "tour-pack.tar.gz")
files = Dir[File.join(outdir, "cards", "*.{svg,png}")].sort.map { |p| ["cards/" + File.basename(p), File.binread(p)] }
files << ["operator-receipt.json", File.binread(File.join(outdir, "operator-receipt.json"))]
files << ["README.txt", "Docent tour pack: 3 personalized postcards (SVG + PNG proof), operator receipt.\nVerify with: tar xzf tour-pack.tar.gz && sha256sum cards/*.svg (compare operator-receipt.json)\n"]
File.open(pack, "wb") do |f|
  gz = Zlib::GzipWriter.new(f); gz.mtime = 0
  Gem::Package::TarWriter.new(gz) { |tar| files.each { |n, d| tar.add_file_simple(n, 0o644, d.bytesize) { |io| io.write(d) } } }
  gz.close
end
puts JSON.pretty_generate(tools: tools, cards: cards.map { |c| c.slice(:file, :inscription, :svg_sha256) }, pack: File.basename(pack), pack_sha256: sha.(File.binread(pack)))
