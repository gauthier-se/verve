# Changelog

## 0.1.0 (2026-10-30)


### Features

* **api:** add the huma api skeleton, meta endpoint and typed client ([#64](https://github.com/gauthier-se/verve/issues/64)) ([e694d3d](https://github.com/gauthier-se/verve/commit/e694d3deb2f0d3707b191e1b1f056ea321261e5a))
* **cli:** bootstrap go module, cli root, clock and taskfile ([#36](https://github.com/gauthier-se/verve/issues/36)) ([c480f97](https://github.com/gauthier-se/verve/commit/c480f97fd44e95cf8b6107d5020ad5e44e2cbaf3))
* **config:** load and validate configuration from defaults, yaml and environment ([#46](https://github.com/gauthier-se/verve/issues/46)) ([9e56544](https://github.com/gauthier-se/verve/commit/9e56544f30abe067d2061a132f99b05ac516b5d5))
* **i18n:** set up i18next with english and french ([#47](https://github.com/gauthier-se/verve/issues/47)) ([c0806c4](https://github.com/gauthier-se/verve/commit/c0806c4c693e6f1ab12e86e4df8a68aa66b08b92))
* **obs:** add health and readiness endpoints and verve healthcheck ([#57](https://github.com/gauthier-se/verve/issues/57)) ([a2409f2](https://github.com/gauthier-se/verve/commit/a2409f21acecbb04a3166e009775cc18993b30dd))
* **obs:** add structured logging, request logging and panic recovery ([#56](https://github.com/gauthier-se/verve/issues/56)) ([5371cca](https://github.com/gauthier-se/verve/commit/5371ccafa3c6c3ef4dd2d10058e8e376f11772d0))
* **server:** add security headers middleware ([#65](https://github.com/gauthier-se/verve/issues/65)) ([6c77637](https://github.com/gauthier-se/verve/commit/6c77637939943e1c9e1e6ec44369972803e4a640))
* **server:** resolve client ip and scheme behind trusted proxies ([#50](https://github.com/gauthier-se/verve/issues/50)) ([a9656be](https://github.com/gauthier-se/verve/commit/a9656be46094473da3fc8b0503b30d43bc150590))
* **server:** serve http with graceful shutdown and a data directory lock ([#48](https://github.com/gauthier-se/verve/issues/48)) ([9e4fa8d](https://github.com/gauthier-se/verve/commit/9e4fa8d5ff842e24bf3a661d132f4184f4978427))
* **server:** serve the embedded spa with client-side routing fallback ([#63](https://github.com/gauthier-se/verve/issues/63)) ([65615ec](https://github.com/gauthier-se/verve/commit/65615ec4afc103071bcad962a038513abee8a7b8))
* **store:** add storage interfaces, unit of work and instance settings ([#54](https://github.com/gauthier-se/verve/issues/54)) ([8064af6](https://github.com/gauthier-se/verve/commit/8064af68911fd72a3a1cc418676e82f482df7d6c))
* **store:** open sqlite with wal and separate writer and reader pools ([#52](https://github.com/gauthier-se/verve/issues/52)) ([4a95886](https://github.com/gauthier-se/verve/commit/4a95886ca0bc206120ef28ebebf89f8676551777))
* **store:** run embedded migrations at startup and with verve migrate ([#53](https://github.com/gauthier-se/verve/issues/53)) ([e9dc678](https://github.com/gauthier-se/verve/commit/e9dc67805a3253412cea92c4171e3208da1dd588))
* **web:** bootstrap vite app with tanstack router and query ([#40](https://github.com/gauthier-se/verve/issues/40)) ([38ea8dd](https://github.com/gauthier-se/verve/commit/38ea8ddd717a40a4993f2a243e49524964c28ded))
* **web:** build the spa shell with version footer and connection screens ([#66](https://github.com/gauthier-se/verve/issues/66)) ([b96dad6](https://github.com/gauthier-se/verve/commit/b96dad61a911807b1eab3c00e3f62aac8db110f9))


### Bug Fixes

* **i18n:** always regenerate translation types ([#68](https://github.com/gauthier-se/verve/issues/68)) ([70caf74](https://github.com/gauthier-se/verve/commit/70caf740da0311706fef4cb72e7f393f83a389d3))
