# Changelog

## [0.4.0](https://github.com/athal7/agentcfg/compare/agentcfg-v0.3.0...agentcfg-v0.4.0) (2026-08-10)


### Features

* initial agentcfg v1 build ([bf04845](https://github.com/athal7/agentcfg/commit/bf0484541acc828afcd00381c0e07731549efea9))
* initial agentcfg v1 build ([caedf32](https://github.com/athal7/agentcfg/commit/caedf32dfee1a33387763d7c7d4b686d00064fed))
* **registry:** support custom agent commands, flat and structured ([#27](https://github.com/athal7/agentcfg/issues/27)) ([7e58123](https://github.com/athal7/agentcfg/commit/7e5812340bcb8b89234ce305c01f6aaf60ea88bb))
* **schema:** harness extra passthrough and cross-harness prompt suffix ([#28](https://github.com/athal7/agentcfg/issues/28)) ([4fe0958](https://github.com/athal7/agentcfg/commit/4fe0958d366cea6faa0922400a7777f4f4cf0397))
* **schema:** opencode_agents persona registry, decoupled from workflow steps ([#29](https://github.com/athal7/agentcfg/issues/29)) ([48079e3](https://github.com/athal7/agentcfg/commit/48079e325dc21d5975f50045fa5c1e9a201da684))


### Bug Fixes

* address CodeRabbit review findings from PR [#1](https://github.com/athal7/agentcfg/issues/1) ([#5](https://github.com/athal7/agentcfg/issues/5)) ([4c26761](https://github.com/athal7/agentcfg/commit/4c26761d344408f83c5dc9a8fa5e4083f8e33155))
* **apply,bashpolicy:** guard empty argv, honor git-lookup failures, write-before-remove, consistent bracket scoring ([8a197ca](https://github.com/athal7/agentcfg/commit/8a197caffdb951e87afa0510bb11d2fbb25ff9ed))
* **apply:** use atomic writes to avoid partial-write corruption on timeout ([da6c509](https://github.com/athal7/agentcfg/commit/da6c509813c209f7e6fb1ef898cefca91e235e00))
* **cli:** guard all init-scaffolded files against overwrite ([da8db87](https://github.com/athal7/agentcfg/commit/da8db8786107d9cad6a237f4c65f6cf19b6f2d80))
* **render/omp:** bash.patterns wire shape was {pattern,decision}, omp expects {match,approval} ([#31](https://github.com/athal7/agentcfg/issues/31)) ([fa4b1b0](https://github.com/athal7/agentcfg/commit/fa4b1b07cbb1b6df194c366235cd5a32cb192cc5))
* **render:** surface the primary agent's dropped edit/write permission for omp ([#22](https://github.com/athal7/agentcfg/issues/22)) ([56ac195](https://github.com/athal7/agentcfg/commit/56ac195c089b9c2a26d6092b977822c6a62822df))
