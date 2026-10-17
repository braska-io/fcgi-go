<?php
// $_COOKIE exactly as PHP parsed HTTP_COOKIE.
require __DIR__ . '/_emit.php';
fixture('app/request/cookie');
emit((object) $_COOKIE);
