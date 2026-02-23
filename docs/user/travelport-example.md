# Case Study: Travelport

<!-- Real-world worked example showing AAT features in context. -->

## Overview

<!-- Travelport JSON API: air search, offer, book, cancel -->
<!-- 59-node graph, OAuth2 auth, complex selections, domain knowledge, addons -->
<!-- This walks through the project as a case study, not a tutorial -->

## Project Structure

<!-- travelport/aat-project.yaml and directory layout -->
<!-- graph.yaml, templates/, env.yaml, domain.yaml, workflows/, plans/ -->

## The API Graph

<!-- Key nodes: CatalogSearch, CatalogOfferingsSearch, OfferSearch, ReservationBuild, ReservationCommit -->
<!-- How nodes map to the booking flow -->
<!-- Array outputs: search results with elementFields (offeringId, productRef) -->

## Authentication

<!-- OAuth2 with client credentials -->
<!-- SecretRef for client_id and client_secret -->

## Selection Strategies in Practice

<!-- How selections pick flights from search results -->
<!-- elementFields: id, departure, price — used by min/max/specific strategies -->
<!-- Named selections for multi-field picks -->

## Workflow Templates

### Base Booking Workflow

<!-- The roundtrip booking flow: search → offer → book → cancel -->

### Addons

<!-- Ancillary addon: add bags/meals to booking -->
<!-- Seat selection addon: pick seats -->
<!-- Traveler modification addon: update passenger details -->

### Composition Example

<!-- Recipe: roundtrip + ancillary + seat → composed plan -->
<!-- Show before (recipe) and after (composed plan) -->

## Domain Knowledge

<!-- Airport codes, cabin classes, passenger types -->
<!-- How pools feed into value resolution -->

## Running the Tests

<!-- aat run batch from travelport/ directory -->
<!-- Example output and archive inspection -->

## Lessons Learned

<!-- Patterns that emerge from a real 59-node graph -->
<!-- When to use addons vs separate workflows -->
<!-- Domain knowledge ROI: where it helped most -->

---

*Source: `docs/user/travelport-example.md` — slimmed, focused on patterns and lessons.*
