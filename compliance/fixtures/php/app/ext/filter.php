<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/filter');
header('Content-Type: text/plain');

$r = match ($q['op'] ?? '') {
    'email_ok' => filter_var('joe@example.com', FILTER_VALIDATE_EMAIL),
    'email_bad' => filter_var('joe@@example.com', FILTER_VALIDATE_EMAIL),
    'int_range_in' => filter_var('5', FILTER_VALIDATE_INT, ['options' => ['min_range' => 1, 'max_range' => 10]]),
    'int_range_out' => filter_var('11', FILTER_VALIDATE_INT, ['options' => ['min_range' => 1, 'max_range' => 10]]),
    'url_ok' => filter_var('https://example.test/path?q=1', FILTER_VALIDATE_URL),
    'ip' => [
        filter_var('::1', FILTER_VALIDATE_IP),
        filter_var('::1', FILTER_VALIDATE_IP, FILTER_FLAG_IPV4),
        filter_var('10.0.0.1', FILTER_VALIDATE_IP, FILTER_FLAG_NO_PRIV_RANGE),
    ],
    default => 'unknown op',
};
echo var_export($r, true);
