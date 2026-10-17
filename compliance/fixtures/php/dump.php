<?php
require __DIR__ . '/_fixture.php';
fixture('dump');

// PHP adds these to $_SERVER itself; they are not FastCGI parameters.
$derived = ['PHP_SELF', 'REQUEST_TIME', 'REQUEST_TIME_FLOAT', 'argv', 'argc',
    'PHP_AUTH_USER', 'PHP_AUTH_PW', 'PHP_AUTH_DIGEST'];
$env = [];
foreach ($_SERVER as $k => $v) {
    if (!in_array($k, $derived, true) && is_string($v)) {
        $env[$k] = $v;
    }
}
$stdin = file_get_contents('php://input');
header('Content-Type: application/json');
echo json_encode([
    'env' => (object) $env,
    'stdin_length' => strlen($stdin),
    'stdin_sha256' => hash('sha256', $stdin),
], JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE);
