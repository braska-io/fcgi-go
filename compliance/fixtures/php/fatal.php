<?php
require __DIR__ . '/_fixture.php';
fixture('fatal');
echo 'before';
throw new RuntimeException('fixture fatal');
