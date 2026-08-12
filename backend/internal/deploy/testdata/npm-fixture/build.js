const fs = require('fs');
const path = require('path');

const distDir = path.join(__dirname, 'dist');
fs.mkdirSync(distDir, { recursive: true });
fs.writeFileSync(path.join(distDir, 'index.html'), '<html><body>deploy fixture build ok</body></html>\n');

// Real frontend builds (e.g. Vite/React) emit nested subdirectories such as
// assets/ — mirror that here so tests can prove copyDir actually recurses
// into subdirectories instead of silently dropping them.
const assetsDir = path.join(distDir, 'assets');
fs.mkdirSync(assetsDir, { recursive: true });
fs.writeFileSync(path.join(assetsDir, 'style.css'), 'body { color: red; }\n');
