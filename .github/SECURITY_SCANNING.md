# Dependency security

The fork's dependency alerts were previously disabled. After enabling them, the compression dependency had a high-severity advisory. The compression library is updated to 1.18.7.

A full govulncheck also identified reachable advisories in the Git, SSH, HTTP and text libraries and the Go standard library. The PR updates these libraries and raises the supported Go patch floor to 1.26.10. The release workflow uses that patched version as well.

The patched Git library exposes a missing packfile as a wrapped filesystem error. Repository-corruption detection now recognizes that error. The corruption test fixture closes and reopens storage so cached objects cannot conceal its deliberately removed packfiles. The existing fail-closed recovery assertions are preserved.

The affected storage/API tests and server compilation are checked without starting the application or accessing configured token stores. Govulncheck with the patched supported toolchain reports zero reachable vulnerabilities. Its report separately identifies advisories in code that the application does not call; scanner reachability is not a blanket claim that every code path or future configuration is safe.

The remaining module-level advisory is GO-2026-5932 for unmaintained golang.org/x/crypto/openpgp. The application does not import that package, and no patched release is listed. The required Security checks gate uses govulncheck at package level, so importing a vulnerable package also blocks CI. This unused package warning is documented, not dismissed as a false positive. Default-branch alerts close after merge and a dependency graph refresh. These findings were not dismissed as false positives.

Made by gpt-6 in Codex
