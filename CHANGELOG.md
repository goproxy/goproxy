# Changelog

## [0.18.1](https://github.com/goproxy/goproxy/compare/v0.18.0...v0.18.1) (2024-12-07)


### Code Refactoring

* utilize `slices` package from stdlib ([#86](https://github.com/goproxy/goproxy/issues/86)) ([b108687](https://github.com/goproxy/goproxy/commit/b108687b51813c7110fde0b6309876f278f6e09a))


### Documentation

* **README.md:** add Conventional Commits requirement to "Contributing" section ([#81](https://github.com/goproxy/goproxy/issues/81)) ([c0ce09d](https://github.com/goproxy/goproxy/commit/c0ce09d6e384a61f7f012589da508f3d48cd738b))


### Miscellaneous Chores

* **ci:** configure release-please to open PRs as drafts ([#83](https://github.com/goproxy/goproxy/issues/83)) ([320a8c1](https://github.com/goproxy/goproxy/commit/320a8c17837c44373511372ac3750bb5d8b25bfe))
* **deps:** bump golang.org/x/mod and github.com/minio/minio-go/v7 ([#85](https://github.com/goproxy/goproxy/issues/85)) ([f44b882](https://github.com/goproxy/goproxy/commit/f44b8827e37dd3636606a42649af7d3750ecc6e3))
* **Dockerfile:** use Alpine 3.21 as base image ([#84](https://github.com/goproxy/goproxy/issues/84)) ([7bb9dfd](https://github.com/goproxy/goproxy/commit/7bb9dfd090ab4faa4f0abccc65f0abdbef942542))

## [0.18.0](https://github.com/goproxy/goproxy/compare/v0.17.2...v0.18.0) (2024-10-26)


### Bug Fixes

* **GoFetcher:** include `latest` query when performing `directList` ([#73](https://github.com/goproxy/goproxy/issues/73)) ([9dad311](https://github.com/goproxy/goproxy/commit/9dad311a82c3984a083ff0598cbed212ea7db38e))


### Code Refactoring

* bump minimum required Go version to 1.22.0 ([#79](https://github.com/goproxy/goproxy/issues/79)) ([7e4176b](https://github.com/goproxy/goproxy/commit/7e4176be1f233a2e069f6313e6ce5407bf2ec05a))


### Tests

* cover Go 1.23 ([#77](https://github.com/goproxy/goproxy/issues/77)) ([b8da543](https://github.com/goproxy/goproxy/commit/b8da543f31677edc2901aedc8a056477a7949c78))


### Miscellaneous Chores

* bump `.goreleaser.yaml` to v2 ([#75](https://github.com/goproxy/goproxy/issues/75)) ([7a75593](https://github.com/goproxy/goproxy/commit/7a75593fc37b82406c3db882bb864dbeb4ebc60c))
* release 0.18.0 ([#80](https://github.com/goproxy/goproxy/issues/80)) ([c985dba](https://github.com/goproxy/goproxy/commit/c985dbaa2025098fa1b671f8366122ecc31bbc33))
* use Go 1.23 for releases ([#78](https://github.com/goproxy/goproxy/issues/78)) ([0b35852](https://github.com/goproxy/goproxy/commit/0b35852a24e3199b6d822bb446e8efa0bf17adb7))

## [0.17.2](https://github.com/goproxy/goproxy/compare/v0.17.1...v0.17.2) (2024-07-09)


### Miscellaneous Chores

* release 0.17.2 ([#70](https://github.com/goproxy/goproxy/issues/70)) ([5bf903a](https://github.com/goproxy/goproxy/commit/5bf903a6a3509c8607b8c1f9bca92b6fa92eb3ce)), closes [#57](https://github.com/goproxy/goproxy/issues/57)

## [0.17.1](https://github.com/goproxy/goproxy/compare/v0.17.0...v0.17.1) (2024-07-05)


### Miscellaneous Chores

* **deps:** bump golang.org/x/mod from 0.18.0 to 0.19.0 ([#68](https://github.com/goproxy/goproxy/issues/68)) ([141fb73](https://github.com/goproxy/goproxy/commit/141fb73d2e6055df46cb99df1b0ac6fba1b15090))

## [0.17.0](https://github.com/goproxy/goproxy/compare/v0.16.10...v0.17.0) (2024-06-23)


### Features

* **Cacher:** support optional `interface{ Size() int64 }` for Content-Length header ([#60](https://github.com/goproxy/goproxy/issues/60)) ([546d218](https://github.com/goproxy/goproxy/commit/546d21817ed7ccf9fd925ee3262ce23dfa4aeb5c))


### Miscellaneous Chores

* **ci:** bump goreleaser/goreleaser-action from 5 to 6 ([#64](https://github.com/goproxy/goproxy/issues/64)) ([afa0f0b](https://github.com/goproxy/goproxy/commit/afa0f0b561da1dd88f9d96aef338df3ec5b6eb1c))
* **ci:** utilize googleapis/release-please-action@v4 ([#62](https://github.com/goproxy/goproxy/issues/62)) ([f2383d6](https://github.com/goproxy/goproxy/commit/f2383d6d93aeb5ed8a7528e1b0076ac7f09276e9))
* **deps:** bump github.com/spf13/cobra from 1.8.0 to 1.8.1 ([#65](https://github.com/goproxy/goproxy/issues/65)) ([39a876c](https://github.com/goproxy/goproxy/commit/39a876c6e55b84f77ebcab792bf7e1ea85a58022))
* **deps:** bump golang.org/x/mod from 0.16.0 to 0.18.0 ([#66](https://github.com/goproxy/goproxy/issues/66)) ([b4c1099](https://github.com/goproxy/goproxy/commit/b4c1099bf0ef93f953abff554eaae979343ee2cf))
* format README.md ([#61](https://github.com/goproxy/goproxy/issues/61)) ([0d2f7d6](https://github.com/goproxy/goproxy/commit/0d2f7d666a486ba7741fd3e39480dc9722a85e6b))
* release 0.17.0 ([#67](https://github.com/goproxy/goproxy/issues/67)) ([c688753](https://github.com/goproxy/goproxy/commit/c6887530ee86bbe7195f61af7002b6c358cc354b))
