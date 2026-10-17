<?php
// Scheme detection: the proxy header as the application reads it, and
// whether PHP considers the request to be HTTPS.
require __DIR__ . '/_json.php';
fixture('app/compat/proto');
$https = $_SERVER['HTTPS'] ?? '';
emit_json([
    'x_forwarded_proto' => $_SERVER['HTTP_X_FORWARDED_PROTO'] ?? null,
    'https' => $https !== '' && strtolower($https) !== 'off',
]);
