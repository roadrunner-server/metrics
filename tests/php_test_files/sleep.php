<?php

declare(strict_types=1);

use Spiral\Goridge\StreamRelay;
use Spiral\RoadRunner\Worker as RoadRunner;

// stdout carries the goridge frames, so diagnostics have to go elsewhere
ini_set('display_errors', 'stderr');
require __DIR__ . "/vendor/autoload.php";

$rr = new RoadRunner(new StreamRelay(\STDIN, \STDOUT));

while($rr->waitPayload()){
    // long enough to keep the only worker busy while the caller checks the
    // queue metrics, short enough to release it before the container is stopped
    sleep(3);
    $rr->respond(new \Spiral\RoadRunner\Payload(""));
}
