const fs = require('fs');
const path = require('path');

// Requiring a dependency that only exists once npm has actually installed
// it (see package.json's "fixture-dep") means this build fails exactly the
// way a real Vite/React build fails when DevPlatform builds a fresh git
// checkout that never had `npm install` run against it: "Cannot find
// module". A build.js that used no dependencies at all would pass whether
// or not the Builder ever installed anything, which is exactly how the
// missing-install bug went unnoticed until a real deploy hit it.
const marker = require('fixture-dep');

const distDir = path.join(__dirname, 'dist');
fs.mkdirSync(distDir, { recursive: true });
fs.writeFileSync(path.join(distDir, 'index.html'), `<html><body>${marker}</body></html>\n`);

// Real frontend builds (e.g. Vite/React) emit nested subdirectories such as
// assets/ — mirror that here so tests can prove copyDir actually recurses
// into subdirectories instead of silently dropping them.
const assetsDir = path.join(distDir, 'assets');
fs.mkdirSync(assetsDir, { recursive: true });
fs.writeFileSync(path.join(assetsDir, 'style.css'), 'body { color: red; }\n');
