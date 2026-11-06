// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {Script, console} from "forge-std/Script.sol";
import {ChamaLedger} from "../src/ChamaLedger.sol";

/// Usage (local Anvil):
///   forge script script/Deploy.s.sol --rpc-url local --broadcast --private-key $CHAIN_PRIVATE_KEY
contract Deploy is Script {
    function run() external {
        vm.startBroadcast();
        // The deployer is also the first relayer (the backend uses the same key to anchor).
        ChamaLedger ledger = new ChamaLedger(msg.sender);
        vm.stopBroadcast();
        console.log("ChamaLedger deployed at:", address(ledger));
    }
}
