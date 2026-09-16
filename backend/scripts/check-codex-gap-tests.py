#!/usr/bin/env python3
"""Run the bounded, offline Codex-gap contract suite and prove tests executed.

Requires the Go toolchain declared by backend/go.mod and Python 3.9+.
No production credentials/endpoints are used. Go may download dependencies.
The caller controls process-local build caches and toolchain environment.
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import subprocess
import sys
import tempfile

REQUIRED = frozenset({
    "TestOpenAIWSConnPool_BackgroundPingSweepToleratesSlowPong",
    "TestOpenAIWSConnReaderLoop_RealConnAnswersServerPingWhileIdle",
    "TestBuildUpstreamTransport_LongStreamH2_NegotiatesHTTP2",
    "TestEnableHTTP2KeepAlive_EnablesPingHealthCheck",
    "TestR1B7ResponsesCompressionDoesNotExpandToUnverifiedSubpaths",
    "TestR1B7ResponsesCompressionScopeExcludesDirectAPIKeyAndOtherProviders",
    "TestR1B7ReconstructedResponsesCompressesCodexBackend",
    "TestR1B7PassthroughResponsesCompressesCodexBackendAndPreservesBodyBytes",
    "TestOpenAICapacityFailoverCarriesSafeTerminalResponse",
    "TestOpenAIResponsesEmptyCompletedFailsOver",
    "TestForwardAsRawChatCompletions_SilentRefusalTriggersFailover",
    "TestOpenAIStreamingPreambleOnlyMissingTerminalReturnsFailover",
    "TestForwardAsAnthropic_MissingTerminalBeforeOutputReturnsFailoverAndOps",
    "TestForwardAsRawChatCompletions_EmptyStreamBeforeOutputTriggersFailover",
    "TestOpenAIResponsesWebSocket_FirstOutputTimeoutAfterDispatchDoesNotReplayAcrossAccounts",
    "TestR1B6ExplicitBusinessFailoverKeepsExistingNextAccountPolicy",
    "TestR1B6FirstOutputTimeoutDoesNotReplayAcrossAccounts",
    "TestR1B6HTTPResponseBodyReadFailureDoesNotReplayAcrossAccounts",
    "TestR1B6TransportFailoverRequiresProvablyPreSendFailure",
    "TestR1B5HandlerPreservesHyphenatedSessionAliasesToFakeUpstream",
    "TestR1B5DefaultLinuxFallbackMatchesLockedReferenceEnvironment",
    "TestR1B5PassthroughPreservesConfiguredOfficialClientSurface",
    "TestR1B5PassthroughUsesHyphenatedIngressSessionAliases",
    "TestR1OpenAIAccountTestDirectAPIKeyPathsBrokenBindingDoNotDispatch",
    "TestR1RequiredProxyAttemptBoundary",
    "TestR1CodexPATWhoamiDoesNotFollowRedirect",
    "TestR1APIKeyResponsesProbeBrokenBindingDoesNotDispatch",
    "TestR1AgentTaskRegistrationPreservesBindingAndRedirectBoundary",
    "TestR1OAuthTokenClientRefusesRedirects",
    "TestR1OAuthTokenRedirectPolicyKeepsNormalSuccess",
    "TestR1RequiredProxyBlocksUnverifiedPlugin",
    "TestR1RequiredProxyKeepsLegacyAndValidRoutes",
    "TestR1RequiredProxyRejectsExpiredDisabledAndAutomaticFallback",
    "TestR1RequiredProxyRejectsLostBinding",
    "TestR1RequiredProxyRejectsMalformedPolicy",
    "TestR1RequiredProxySurvivesAdministrativeReload",

    "TestParse_ErrorsDoNotExposeCredentials",
    "TestOpenAIQuotaBoundProxyFailsClosed",
    "TestOpenAIQuotaProxyFailurePrecedesTokenLookup",
    "TestOpenAIQuotaProxyResolutionPreservesValidRoutes",
    "TestOpenAIQuotaProxyContractReachesFakeUpstream",
    "TestOpenAIQuotaShadowUsesParentProxyBinding",
    "TestCodexGapExecIdentityUsesExistingOverride",
    "TestCodexGapConvergenceOffIsIndependentOfIdentity",
})
REGRESSION = frozenset({
    "TestEnsureCodexIdentityHeaders",
    "TestEnforceCodexIdentityHeaders",
    "TestEnforceCodexIdentityHeadersWithAccountOverrideUA",
    "TestEnforceCodexIdentityHeadersFollowsCanonicalResolver",
    "TestEnforceCodexIdentityHeadersRejectsInvalidCanonicalUA",
    "TestPrepareUpstreamCallShadowResolve",
    "TestResetCreditShadowRejected",
    "TestQueryUsageShadowResolve_EndToEnd",
    "TestQueryUsageResetCreditCountPrecedence",
    "TestResetCreditTargetedSendsStableCreditAndRedeemIDs",
})
BATCH2_REQUIRED = frozenset({
    "TestGapV2GatewayTokenPreflight",
    "TestGapV2HTTPDispatchBinding",
    "TestGapV2HTTPUnboundAndOtherPlatformsUnchanged",
    "TestGapV2ModelsBrokenBindingDoesNotDispatch",
    "TestGapV2ModelsShadowUsesCredentialRoute",
    "TestGapV2OAuthBoundProxyFailsClosed",
    "TestGapV2OAuthRawProxyValidation",
    "TestGapV2OAuthValidRoutesAndStatePreserved",
    "TestGapV2PrivacyBoundProxyFailsClosed",
    "TestGapV2PrivacyValidRoutesAndSkipPreserved",
    "TestGapV2WSBrokenBindingBeforeDialOrPrewarm",
    "TestGapV2WSPoolRouteChangeDoesNotReuseConnection",
    "TestGapV2WSQueuedProxySnapshotIsIndependent",
})
ALL_REQUIRED = REQUIRED | REGRESSION | BATCH2_REQUIRED


def validate_log(path: Path, exit_code: int = 0) -> dict:
    """Exit 0, cached output, or a skipped test alone must not pass this gate."""
    ran, passed, skipped, failed = set(), set(), set(), set()
    package_passes = set()
    total_passes = 0
    with path.open(encoding="utf-8", errors="replace") as stream:
        for line in stream:
            try:
                event = json.loads(line)
            except ValueError:
                continue  # compiler and dependency diagnostics are not JSON
            action, test = event.get("Action"), event.get("Test")
            if test:
                if action == "run": ran.add(test)
                elif action == "pass": passed.add(test); total_passes += 1
                elif action == "skip": skipped.add(test)
                elif action == "fail": failed.add(test)
            elif action == "fail":
                failed.add(event.get("Package", "unknown-package"))
            elif action == "pass":
                package_passes.add(event.get("Package"))
    missing = ALL_REQUIRED - (ran & passed)
    expected_packages = {
        "github.com/Wei-Shaw/sub2api/internal/handler",
        "github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl",
        "github.com/Wei-Shaw/sub2api/internal/repository",
        "github.com/Wei-Shaw/sub2api/internal/service",
    }
    success = (exit_code == 0 and not missing and not failed
               and not (ALL_REQUIRED & skipped)
               and expected_packages <= package_passes)
    return {"passed": success, "go_exit_code": exit_code,
            "required_count": len(ALL_REQUIRED),
            "required_executed_and_passed": len(ALL_REQUIRED & ran & passed),
            "missing_required": sorted(missing), "failed": sorted(failed),
            "skipped_required": sorted(ALL_REQUIRED & skipped),
            "top_level_passed": len({n for n in passed if "/" not in n}),
            "subtests_passed": total_passes - len({n for n in passed if "/" not in n}),
            "package_passes": sorted(package_passes), "log": str(path)}


def self_test() -> None:
    with tempfile.TemporaryDirectory(prefix="codex-gap-gate-") as temp:
        path = Path(temp) / "test.jsonl"
        path.write_text("", encoding="utf-8")
        assert not validate_log(path)["passed"], "zero tests must fail"
        events = []
        for name in sorted(ALL_REQUIRED):
            events += [{"Action": "run", "Test": name}, {"Action": "pass", "Test": name}]
        for package in ["internal/handler", "internal/pkg/proxyurl", "internal/service", "internal/repository"]:
            events.append({"Action": "pass", "Package": "github.com/Wei-Shaw/sub2api/" + package})
        def save(data): path.write_text("\n".join(json.dumps(e) for e in data), encoding="utf-8")
        save(events)
        assert validate_log(path)["passed"]
        assert not validate_log(path, 1)["passed"], "nonzero exit must fail"
        skipped = [dict(e) for e in events]
        next(e for e in skipped if e.get("Action") == "pass" and e.get("Test"))["Action"] = "skip"
        save(skipped)
        assert not validate_log(path)["passed"], "required skipped test must fail"
        save([e for e in events if e.get("Action") != "run"])
        assert not validate_log(path)["passed"], "pass without run must fail"
        save(events + [{"Action":"fail", "Package":"build-failure"}])
        assert not validate_log(path)["passed"], "build failure must fail"
    print("gate self-test passed: empty/skip/pass-without-run/nonzero/build-failure rejected")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default="go", help="Path to a compatible Go executable")
    parser.add_argument("--log", type=Path, default=Path(tempfile.gettempdir()) / "codex-gap-tests.jsonl")
    parser.add_argument("--verify-log", type=Path, help="Validate an existing go test -json log without executing tests")
    parser.add_argument("--exit-code", type=int, default=0, help="Go exit code when --verify-log is used")
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        self_test()
        return 0
    code = args.exit_code
    path = args.verify_log or args.log
    if not args.verify_log:
        path.parent.mkdir(parents=True, exist_ok=True)
        pattern = "^(TestParse_.*|" + "|".join(sorted(ALL_REQUIRED)) + ")$"
        command = [args.go, "test", "-p=4", "-mod=readonly", "-count=1", "-tags=unit",
                   "-json", "-timeout=180s", "-run", pattern,
                   "./internal/handler", "./internal/pkg/proxyurl", "./internal/service", "./internal/repository"]
        print("Running bounded offline contract suite; log=" + str(path), flush=True)
        with path.open("w", encoding="utf-8") as output:
            try:
                result = subprocess.run(command, cwd=Path(__file__).resolve().parents[1],
                                        stdout=output, stderr=subprocess.STDOUT, timeout=900)
                code = result.returncode
            except subprocess.TimeoutExpired:
                code = 124
    result = validate_log(path, code)
    print(json.dumps(result, indent=2, ensure_ascii=False))
    return 0 if result["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
