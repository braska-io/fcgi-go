<?php
// PHP's own view of the credentials: what it derived from Authorization.
require __DIR__ . '/_emit.php';
fixture('app/request/auth');
emit([
    'PHP_AUTH_USER' => $_SERVER['PHP_AUTH_USER'] ?? null,
    'PHP_AUTH_PW' => $_SERVER['PHP_AUTH_PW'] ?? null,
    'HTTP_AUTHORIZATION' => $_SERVER['HTTP_AUTHORIZATION'] ?? null,
]);
