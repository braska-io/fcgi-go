<?php
// Counts requests within one session; persistence across requests needs the cookie.
require __DIR__ . '/../../_fixture.php';
fixture('app/session/counter');

session_start();
$_SESSION['n'] = ($_SESSION['n'] ?? 0) + 1;
echo 'n=' . $_SESSION['n'];
