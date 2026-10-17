<?php
// Kills the session the way the session_destroy() manual page shows.
require __DIR__ . '/../../_fixture.php';
fixture('app/session/destroy');

session_start();
$_SESSION = [];
if (ini_get('session.use_cookies')) {
    $params = session_get_cookie_params();
    setcookie(
        session_name(),
        '',
        time() - 42000,
        $params['path'],
        $params['domain'],
        $params['secure'],
        $params['httponly']
    );
}
$ok = session_destroy();
echo 'destroyed=' . ($ok ? 'true' : 'false') . ' status=' . (session_status() === PHP_SESSION_NONE ? 'none' : 'other');
