<?php
// A fatal error before any output, as a missing class or function causes.
require __DIR__ . '/../../_fixture.php';
fixture('app/compat/fatal');
header('Content-Type: text/html; charset=utf-8');
compat_fixture_undefined_function();
