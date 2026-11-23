const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const ase = require('ase-utils');
const root = path.resolve(process.argv[2]);
const colors = JSON.parse(fs.readFileSync(path.join(root,'selection.json')));
const result = ase.decode(fs.readFileSync(path.join(root,'venetian-lagoon.ase')));
assert.equal(result.version,'1.0');
assert.equal(result.colors.length,6);
for (const [i, entry] of result.colors.entries()) {
  assert.equal(entry.name,colors[i].name);
  assert.equal(entry.model,'RGB');
  assert.deepEqual(entry.color.map(component=>Math.round(255*component)),colors[i].rgb);
}
console.log('ase-utils decoded six matching RGB names and values.');
