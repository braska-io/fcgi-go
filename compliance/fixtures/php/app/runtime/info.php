<?php
// Facts about the runtime the request runs in: SAPI, version, opcache, env.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/runtime/info');
header('Content-Type: text/plain');

$b = static fn (bool $v): string => $v ? 'true' : 'false';

echo match ($q['op'] ?? '') {
    'sapi' => php_sapi_name(),
    'version' => PHP_MAJOR_VERSION . '.' . PHP_MINOR_VERSION,
    'request_time' => (function () use ($b): string {
        $rt = $_SERVER['REQUEST_TIME'] ?? null;
        $rtf = $_SERVER['REQUEST_TIME_FLOAT'] ?? null;
        $now = microtime(true);
        $ok = is_int($rt) && is_float($rtf)
            && $rt <= time() && time() - $rt <= 5
            && $rtf <= $now && (int) $rtf === $rt;
        return $b($ok);
    })(),
    'opcache' => (function () use ($b): string {
        $s = opcache_get_status(false);
        return $b($s !== false && $s['opcache_enabled'] === true);
    })(),
    'apcu' => $b(function_exists('apcu_fetch')) . ',' . $b(extension_loaded('apcu')),
    'timezone' => date_default_timezone_get(),
    'date_epoch' => date('Y-m-d H:i:s T', 0),
    'getenv_state' => var_export(getenv('FIXTURE_STATE'), true),
    'getenv_cleared' => var_export(getenv('PATH'), true) . ',' . var_export(getenv('PHPIZE_DEPS'), true),
    default => (function (): string {
        http_response_code(400);
        return 'unknown op';
    })(),
};
