<?php
// Starts a session with a fixed id so the Set-Cookie value is predictable.
// Strict mode would replace a caller-chosen id, so it is off for this script only.
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/session/fixed');

ini_set('session.use_strict_mode', '0');
if (isset($q['name'])) {
    session_name($q['name']);
}
if (isset($q['limiter'])) {
    session_cache_limiter($q['limiter']);
}
$params = [];
foreach (['path', 'domain', 'samesite'] as $k) {
    if (isset($q[$k])) {
        $params[$k] = $q[$k];
    }
}
if (isset($q['secure'])) {
    $params['secure'] = $q['secure'] === '1';
}
if (isset($q['httponly'])) {
    $params['httponly'] = $q['httponly'] === '1';
}
if ($params) {
    session_set_cookie_params($params);
}
session_id($q['id'] ?? 'fcgifixture0001');
session_start();
$_SESSION = [];
echo session_name() . '=' . session_id();
